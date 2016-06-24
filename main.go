package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"path"
	"io/ioutil"
	"path/filepath"
)

var (
	root = "/data"
	mod = map[string]string{
		"maven":"http://repo1.maven.org/maven2",
		"gradle":"http://downloads.gradle.org/distributions",
	}
)

func main() {
	mux:=http.NewServeMux()
	mux.HandleFunc("/maven/", handler)
	mux.HandleFunc("/gradle/", handler)
	log.Println("Start serving on port 80")
	if e := http.ListenAndServe(":80", mux); e != nil {
		log.Println(e)

	}
	os.Exit(0)
}

func handler(w http.ResponseWriter, r *http.Request) {
	uri := r.URL.Path
	log.Println(uri)
	isFile := false
	proxyMod := ""
	if strings.HasPrefix(uri, "/maven") {
		proxyMod = "maven"
	} else if strings.HasPrefix(uri, "/gradle") {
		proxyMod = "gradle"
	}else{
		w.WriteHeader(404)
		return
	}
	realUri := strings.TrimPrefix(uri, "/" + proxyMod)
	filename:=root+"/"+ proxyMod+ realUri
	if path.Ext(realUri) != "" {
		file, err := os.Open(filename)
		if err == nil {
			if bytes, e := ioutil.ReadAll(file); e == nil {
				w.Write(bytes)
				return
			}
		}
		isFile = true
	}
	if code,buf, respErr := doGet(mod[proxyMod] + realUri); respErr == nil {
		if isFile && code==200 {
			writeFile(buf, filename)
		}
		w.Write(buf)
	} else {
		w.WriteHeader(500)
		log.Panicln(respErr)
		return
	}
}
func doGet(url string) (code int,bytes []byte, err error) {
	log.Println(url)
	resp, err := http.Get(url)
	if err!=nil {
		log.Panic(err)
	}
	code=resp.StatusCode
	defer resp.Body.Close()
	bytes ,err= ioutil.ReadAll(resp.Body)
	return
}
func writeFile(bytes []byte, filename string) {
	os.MkdirAll(filepath.Dir(filename), os.ModePerm)
	ioutil.WriteFile(filename, bytes, 0666)
}