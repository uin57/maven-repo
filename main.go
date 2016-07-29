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
	downChan = make(chan *handle, 5)
	lock sync.Mutex
	errTemplate *template.Template
)

func init() {
	flag.StringVar(&token, "token", "", "密码")
	flag.StringVar(&root, "root", "/data", "存储路径")
	flag.StringVar(&addr, "addr", ":80", "监听地址")
	flag.Parse()
	token = base64Coder.EncodeToString([]byte(token))
	tp := template.New("404 template")
	errTemplate, _ = tp.Parse(`{{.url}}&nbsp;{{.e}}<br/>`)
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
	w.WriteHeader(lastStatusCode)
	buffer.WriteTo(w)
}

func writeFile(file string, reader io.Reader) error {
	os.MkdirAll(filepath.Dir(file), os.ModePerm)
	if f, err := os.OpenFile(file, os.O_WRONLY | os.O_CREATE | os.O_TRUNC, 0666); err != nil {
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
				if err := g.GetFile(tempFile, h.url); err != nil {
					h.err = err
					h.ok = false
				} else {
					os.Rename(tempFile, h.savePath)
				}
				close(h.c)
				delete(tasks, h.key)
			}

		}
	}
}