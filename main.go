package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var (
	root = "/data"
	mod  = map[string]string{
		"maven":  "http://repo1.maven.org/maven2",
		"gradle": "http://downloads.gradle.org/distributions",
	}
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Println("unsupport URL:", r.URL.Path)
	})
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
	proxyMod := ""
	if strings.HasPrefix(uri, "/maven") {
		proxyMod = "maven"
	} else if strings.HasPrefix(uri, "/gradle") {
		proxyMod = "gradle"
	} else {
		w.WriteHeader(404)
		return
	}
	realUri := strings.TrimPrefix(uri, "/"+proxyMod)
	filename := root + "/" + proxyMod + realUri
	if exist(filename) {
		http.ServeFile(w, r, filename)
		return
	}
	log.Println(mod[proxyMod] + realUri)
	if resp, err := http.Get(mod[proxyMod] + realUri); err != nil {
		w.WriteHeader(500)
		log.Println(err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			if filepath.Ext(filename) != "" {
				os.MkdirAll(filepath.Dir(filename), os.ModePerm)
				tempFile := filename + ".tmp"
				if err := writeFile(tempFile, resp.Body); err != nil {
					io.Copy(w, resp.Body)
				} else {
					os.Rename(tempFile, filename)
					http.ServeFile(w, r, filename)
				}
			} else {
				io.Copy(w, resp.Body)
			}
		} else {
			w.WriteHeader(resp.StatusCode)
		}
	}
}

func writeFile(file string, reader io.Reader) error {
	if f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0666); err != nil {
		return err
	} else {
		defer f.Close()
		io.Copy(f, reader)
		f.Sync()
		return nil
	}
}

func exist(filename string) bool {
	fi, err := os.Stat(filename)
	return err == nil || (os.IsExist(err) && !fi.IsDir())
}
