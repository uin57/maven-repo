package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"flag"
	"encoding/base64"
	"sync"
	"time"
	"html/template"
	"bytes"
	"io/ioutil"
	"fmt"
	"errors"
	"sync/atomic"
	"net/url"
	"crypto/sha1"
	"encoding/hex"
	"regexp"
)

type urlMeta struct {
	url      string
	proxyURL string
}

var (
	root = "/data"
	token string
	addr string
	mod = map[string][]urlMeta{
		"maven": []urlMeta{
			{"http://central.maven.org/maven2/", "http://wifis:proxy@104.168.94.138:1999"},
			{"http://central.maven.org/maven2/", ""},
			{"http://repo1.maven.org/maven2", "http://wifis:proxy@104.168.94.138:1999"},
			{"http://repo1.maven.org/maven2", ""},
		},
		"gradle": []urlMeta{
			{"http://downloads.gradle.org/distributions", "http://wifis:proxy@104.168.94.138:1999" },
			{"http://downloads.gradle.org/distributions", "" },
		},
	}
	client = &http.Client{
		Timeout:time.Second * 15,
	}
	base64Coder = base64.StdEncoding

	tasks = make(map[string]*handle)
	downChan chan *handle
	lock sync.Mutex
	errTemplate *template.Template
	workers int
	queue int
	blockSize = 1024 * 1024;
)

func init() {
	flag.StringVar(&token, "token", "", "密码")
	flag.StringVar(&root, "root", "/data", "存储路径")
	flag.StringVar(&addr, "addr", ":80", "监听地址")
	flag.IntVar(&workers, "work", 10, "并发下载数")
	flag.IntVar(&queue, "queue", 5, "同时任务数")
	flag.Parse()
	token = base64Coder.EncodeToString([]byte(token))
	tp := template.New("404 template")
	errTemplate, _ = tp.Parse(`{{.url}}&nbsp;{{.e}}<br/>`)
	downChan = make(chan *handle, queue)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/maven/", handler)
	mux.HandleFunc("/gradle/", handler)
	mux.HandleFunc("/upload/", upload)
	log.Printf("Addr: %s \n", addr)
	log.Println("Token:", token)
	log.Println("Root:", root)
	go Downloader()
	if e := http.ListenAndServe(addr, mux); e != nil {
		log.Println(e)
	}
	os.Exit(0)
}

func handler(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Path
	log.Println(uri)
	if strings.HasPrefix(uri, "/maven") {
		handlerM("maven", w, r)
	} else if strings.HasPrefix(uri, "/gradle") {
		handlerM("gradle", w, r)
	} else {
		w.WriteHeader(404)
	}
}

func handlerM(key string, w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Path
	realUri := strings.TrimPrefix(uri, "/" + key)
	filename := root + "/" + key + realUri
	if exist(filename) {
		http.ServeFile(w, r, filename)
		return
	}
	lastStatusCode := 0
	buffer := bytes.NewBuffer(make([]byte, 1024))
	for _, base := range mod[key] {
		GetUrl := base.url + realUri;
		if filepath.Ext(filename) != "" {
			h := download(realUri, GetUrl, base.proxyURL, filename)
			if h.wait() {
				http.ServeFile(w, r, filename)
				return
			}
			lastStatusCode = 404
			errTemplate.Execute(buffer, map[string]interface{}{
				"url":GetUrl,
				"e":h.error(),
			})
		} else {
			if resp, err := client.Get(GetUrl); err != nil {
				lastStatusCode = 500
				log.Println(err, GetUrl)
			} else {
				lastStatusCode = 200
				io.Copy(w, resp.Body)
				return
			}
		}
	}
	log.Println(" ret ", uri, lastStatusCode)
	w.WriteHeader(lastStatusCode)
	//buffer.WriteTo(w)
}

func writeFile(file string, reader io.Reader) error {
	if f, err := touchFile(file); err != nil {
		return err
	} else {
		defer f.Close()
		if _, e := io.Copy(f, reader); e != nil {
			log.Fatal(e)
			return e
		}
		f.Sync()
		return nil
	}
}

func exist(filename string) bool {
	fi, err := os.Stat(filename)
	return err == nil || (os.IsExist(err) && !fi.IsDir())
}

func upload(w http.ResponseWriter, r *http.Request) {
	realUri := strings.TrimPrefix(r.RequestURI, "/upload")
	if "GET" == r.Method {
		log.Println(root + "/maven" + realUri)
		http.ServeFile(w, r, root + "/maven" + realUri)
	} else if "PUT" == r.Method {
		if auth(r, token) {
			defer r.Body.Close()
			if fileErr := writeFile(root + "/maven" + realUri, r.Body); fileErr == nil {
				w.WriteHeader(200)
				return
			} else {
				log.Println("WirteFileError:", fileErr)
			}
		}
		w.WriteHeader(403)
	}
}

func auth(r *http.Request, token string) bool {
	if "" == token {
		return true
	}
	authorization := r.Header.Get("Authorization")
	if token == strings.TrimPrefix(authorization, "Basic ") {
		return true
	}
	return false
}

type h interface {
	wait() bool
	error() error
}

type handle struct {
	c                            chan int
	ok                           bool
	err                          error
	key, url, proxyUrl, savePath string
}

func (h *handle) wait() bool {
	<-h.c
	return h.ok
}
func (h *handle) error() error {
	<-h.c
	return h.err
}

func download(key, url, proxyUrl, savePath string) h {
	lock.Lock()
	defer lock.Unlock()
	if v, ok := tasks[key]; ok {
		return v
	}
	c := make(chan int)
	h := &handle{
		c:c,
		ok:true,
		key:key,
		url:url,
		savePath:savePath,
		proxyUrl:proxyUrl,
	}
	tasks[key] = h
	downChan <- h
	return h
}
func Downloader() {
	for {
		select {
		case h := <-downChan:
			{
				tempFile := h.savePath + ".downloading"
				if err := work(tempFile, h.url, h.proxyUrl, workers); err != nil {
					log.Println("Error", h.url, err)
					h.err = err
					h.ok = false
				} else {
					os.Rename(tempFile, h.savePath)
				}
				close(h.c)
				delete(tasks, h.key)
				os.Remove(tempFile)
			}

		}
	}
}

func touchFile(fileName string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(fileName), os.ModePerm); err != nil {
		return nil, err
	}
	targetFile, opFileErr := os.OpenFile(fileName, os.O_WRONLY | os.O_CREATE | os.O_TRUNC, 0666)
	if opFileErr != nil {
		return nil, opFileErr
	}
	return targetFile, nil
}

func work(fileName, fileUrl, proxyUrl  string, workers int) error {
	res, httpHeadErr := http.Head(fileUrl); // 187 MB file of random numbers per line
	if httpHeadErr != nil {
		return httpHeadErr
	}
	contentLength := int(res.ContentLength)
	if res.StatusCode >= 300 {
		return errors.New("response code: " + res.Status)
	}
	var client *http.Client
	if (proxyUrl == "") {
		client = &http.Client{}
	} else {
		proxy, _ := url.Parse(proxyUrl)
		client = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(proxy),
			},
		}
	}
	targetFile, opFileErr := touchFile(fileName)
	defer targetFile.Close()
	if opFileErr != nil {
		return opFileErr
	}
	//小于20K 单线程
	if contentLength <= blockSize || res.Header.Get("Accept-Ranges") != "bytes" {
		log.Printf("Single Thread Downloading From: %s Length: %d proxy: %s \n", fileUrl, contentLength, proxyUrl)
		var s = worker{
			url:fileUrl,
			client:client,
			targetFile:targetFile,
		}
		if e := s.simpleDownload(); e == nil {
			if sameSha1(fileUrl, fileName) {
				return nil
			} else {
				return errors.New("Sha1 check fail")
			}
		} else {
			return e
		}
	}
	log.Printf("Multi Thread Downloading From: %s Length: %d proxy: %s \n", fileUrl, contentLength, proxyUrl)
	blockCount := contentLength / blockSize
	lastBlockSize := contentLength % blockSize

	workerChan := make(chan int, workers)
	w := worker{
		url:fileUrl,
		client:client,
		workerChan:workerChan,
		wg:&sync.WaitGroup{},
		targetFile:targetFile,
		errorCount:0,
	}
	for i := 0; i < blockCount; i++ {
		min := blockSize * i // Min range
		max := blockSize * (i + 1) // Max range
		if (i == blockCount - 1) {
			max += lastBlockSize // Add the remaining bytes in the last request
		}
		workerChan <- i
		w.wg.Add(1)

		if atomic.LoadInt32(&w.errorCount) == 0 {
			go w.worker(min, max - 1, i)
		}
	}
	w.wg.Wait()
	close(workerChan)
	if w.errorCount == 0 {
		targetFile.Sync()
		if sameSha1(fileUrl, fileName) {
			return nil
		} else {
			return errors.New("Sha1 check fail")
		}
		return nil
	} else {
		return errors.New("下载失败")
	}
	return nil
}

type worker struct {
	url        string
	workerChan <-chan int
	wg         *sync.WaitGroup
	targetFile *os.File
	errorCount int32
	client     *http.Client
}

func (w worker) simpleDownload() error {
	req, _ := http.NewRequest("GET", w.url, nil)
	if resp, err := w.client.Do(req); err == nil {
		defer resp.Body.Close()
		bytes, _ := ioutil.ReadAll(resp.Body)
		w.targetFile.Write(bytes)
		w.targetFile.Sync()
		return nil
	} else {
		return err
	}
}

func (w worker) worker(min, max, i int) {
	req, _ := http.NewRequest("GET", w.url, nil)
	req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", min, max))
	//retry 3 times
	retryTime := 0
	for ; retryTime < 3; retryTime++ {
		if resp, err := w.client.Do(req); err == nil {
			defer resp.Body.Close()
			reader, _ := ioutil.ReadAll(resp.Body)
			w.targetFile.WriteAt(reader, int64(min))
			w.targetFile.Sync()
			break
		}
	}
	if retryTime != 3 {
		atomic.AddInt32(&w.errorCount, 1)
	}
	w.wg.Done()
	<-w.workerChan
}

func sameSha1(url string, filePath string) bool {
	if b, regErr := regexp.MatchString("\\.sha1$", url); b && regErr == nil {
		return true
	}
	hash := sha1.New()
	hash.Reset()
	if f, fileErr := os.Open(filePath); fileErr == nil {
		defer f.Close()
		io.Copy(hash, f)
		if resp, respErr := http.Get(url + ".sha1"); respErr == nil {
			defer resp.Body.Close()
			b, _ := ioutil.ReadAll(resp.Body)
			hashCode := hex.EncodeToString(hash.Sum(nil))
			serverHash := string(b)
			return serverHash == hashCode
		}
	}
	return false
}