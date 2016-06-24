package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path"
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
		log.Println("其他URL", r.URL.Path)
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
	isFile := false
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
	if path.Ext(realUri) != "" {
		file, err := os.Open(filename)
		defer file.Close()
		if err == nil {
			if _, err := io.Copy(w, file); err != nil {
				w.WriteHeader(500)
			}
			return
		}
		isFile = true
	}
	log.Println(mod[proxyMod] + realUri)
	if resp, err := http.Get(mod[proxyMod] + realUri); err != nil {
		w.WriteHeader(500)
		log.Println(err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode == 200 {
			if isFile {
				os.MkdirAll(filepath.Dir(filename), os.ModePerm)
				tempFile := filename + ".tmp"
				if err := writeFile(tempFile, resp.Body); err != nil {
					io.Copy(w, resp.Body)
				} else {
					os.Rename(tempFile, filename)
					f, _ := os.Open(filename)
					defer func() {
						f.Close()
					}()
					if _, err := io.Copy(w, f); err != nil {
						w.WriteHeader(500)
					}
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

func doGet(url string, writer io.Writer) (code int, bytes []byte, err error) {
	log.Println(url)
	resp, err := http.Get(url)
	if err != nil {
		log.Panic(err)
	}
	code = resp.StatusCode
	defer resp.Body.Close()
	_, err = io.Copy(writer, resp.Body)
	return
}
