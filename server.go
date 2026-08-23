package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	d := "/lzcapp/pkg/content/web"
	if _, e := os.Stat(d); e != nil {
		d = "web"
	}
	fs := http.FileServer(http.Dir(d))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := d + r.URL.Path
		if _, e := os.Stat(p); os.IsNotExist(e) {
			http.ServeFile(w, r, d+"/index.html")
			return
		}
		fs.ServeHTTP(w, r)
	})
	log.Println("猫眼 listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
