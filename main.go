package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"flag"
	g "github.com/hashicorp/go-getter"
	"encoding/base64"
	"sync"
	"time"
	"html/template"
	"bytes"
	"io/ioutil"
	"fmt"
	"errors"
)

var (
	root = "/data"
	token string
	addr string
	mod = map[string][]string{
		"maven": []string{"http://repo1.maven.org/maven2", "http://mirrors.ibiblio.org/pub/mirrors/maven2"},
		"gradle": []string{"http://downloads.gradle.org/distributions"},
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
	limitSize int
)

func init() {
	flag.StringVar(&token, "token", "", "密码")
	flag.StringVar(&root, "root", "/data", "存储路径")
	flag.StringVar(&addr, "addr", ":80", "监听地址")
	flag.IntVar(&workers, "work", 10, "并发下载数")
	flag.IntVar(&queue, "queue", 5, "同时任务数")
	flag.IntVar(&limitSize, "limit", 20, "单线程下载最小限制")
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
		GetUrl := base + realUri;
		if filepath.Ext(filename) != "" {
			h := download(realUri, GetUrl, filename)
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
	c                  chan int
	ok                 bool
	err                error
	key, url, savePath string
}

func (h *handle) wait() bool {
	<-h.c
	return h.ok
}
func (h *handle) error() error {
	<-h.c
	return h.err
}

func download(key, url, savePath string) h {
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
				log.Printf("Downloading From: %s \n", h.url)
				tempFile := h.savePath + ".downloading"
				if err := work(tempFile, h.url, workers); err != nil {
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

func work(fileName, url  string, limit int) error {
	res, httpHeadErr := http.Head(url); // 187 MB file of random numbers per line

	if httpHeadErr != nil {
		return httpHeadErr
	}

	if res.StatusCode >= 300 {
		return errors.New("response code: " + res.Status)
	}
	//小于20K 单线程
	if res.ContentLength < int64(1024 * limitSize) || res.Header.Get("Accept-Ranges") != "bytes" {
		log.Println("start single Thread download ", url)
		return g.GetFile(fileName, url)
	}
	var wg sync.WaitGroup
	targetFile, opFileErr := touchFile(fileName)
	if opFileErr != nil {
		return opFileErr
	}
	defer targetFile.Close()
	len_sub := int(res.ContentLength) / limit // Bytes for each Go-routine
	diff := int(res.ContentLength) % limit // Get the remaining for the last request
	allDone := true
	worker := func(min int, max int, i int) {
		client := &http.Client{}
		//retry 3 times
		for r := 0; r < 3; r++ {
			req, respErr := http.NewRequest("GET", url, nil)
			if respErr == nil {
				log.Println("start download ", url, fmt.Sprintf("bytes=%d-%d", min, max))
				req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", min, max))
				resp, _ := client.Do(req)
				defer resp.Body.Close()
				reader, _ := ioutil.ReadAll(resp.Body)
				targetFile.WriteAt(reader, int64(min))
				break
			} else {
				allDone = false
			}
		}
		wg.Done()
	}
	for i := 0; i < limit; i++ {
		wg.Add(1)
		min := len_sub * i // Min range
		max := len_sub * (i + 1) // Max range
		if (i == limit - 1) {
			max += diff // Add the remaining bytes in the last request
		}
		go worker(min, max - 1, i)
	}
	wg.Wait()
	targetFile.Sync()
	if allDone {
		return nil
	} else {
		return errors.New("下载失败")
	}
	return nil
}
