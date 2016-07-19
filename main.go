package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"encoding/base64"
	"flag"
)

var (
	root = "/data"
	mod = map[string][]string{
		"maven": []string{"http://repo1.maven.org/maven2", "http://central.maven.org/maven2"},
		"gradle": []string{"http://downloads.gradle.org/distributions"},
	}
	client = &http.Client{
		//Timeout:time.Second * 15,
	}
	base64Coder = base64.StdEncoding
	token string
	inWall bool
)

func init() {
	flag.StringVar(&token, "token", "", "密码")
	flag.StringVar(&root, "root", "/data", "路径")
	flag.BoolVar(&inWall, "root", "true", "路径")
	flag.Parse()
	token = base64Coder.EncodeToString([]byte(token))
	if inWall {
		mod["maven"] = append([]string{"http://maven.oschina.net/content/groups/public"}, mod["maven"]...)
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/maven/", handler)
	mux.HandleFunc("/gradle/", handler)
	mux.HandleFunc("/upload/", upload)
	log.Println("Port: 80")
	log.Println("Token:", token)
	log.Println("Root:", root)
	if e := http.ListenAndServe(":80", mux); e != nil {
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
	for _, base := range mod[key] {
		GetUrl := base + realUri;
		log.Println(GetUrl)
		if resp, err := client.Get(GetUrl); err != nil {
			lastStatusCode = 500
			log.Println(err, GetUrl)
		} else {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				if filepath.Ext(filename) != "" {
					tempFile := filename + ".tmp"
					if err := writeFile(tempFile, resp.Body); err != nil {
						io.Copy(w, resp.Body)
						log.Fatalln(err)
					} else {
						os.Rename(tempFile, filename)
						http.ServeFile(w, r, filename)
					}
				} else {
					io.Copy(w, resp.Body)
				}
				return
			} else {
				lastStatusCode = resp.StatusCode
				log.Println(resp.StatusCode, GetUrl)
			}
		}
	}
	w.WriteHeader(lastStatusCode)
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
				log.Fatalln("WirteFileError:", fileErr)
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