package main

import (
	"errors"
	"html/template"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"
)

var tmplRoot = template.Must(template.New("Root").Parse(`
<!DOCTYPE html>
<html lang="en">
	<head>
		<meta charset="utf-8">
		<title>Updown</title>
	</head>
	<body>
		<h1>Updown</h1>
		<p>Welcome to updown.</p>
		<form method="post" action="/upload" enctype="multipart/form-data">
			<input type="file" name="file">
			<button type="submit">Upload</button>
		</form>
	</body>
</html>
`))

func handleRootGet(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		err := tmplRoot.Execute(w, nil)
		if err != nil {
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	default:
		http.NotFound(w, r)
	}
}

func handleUploadPost(w http.ResponseWriter, r *http.Request) {
	readPart := func(part *multipart.Part) (ok bool, err error) {
		defer func(part *multipart.Part) {
			err := part.Close()
			if err != nil {
				log.Printf("Unable to close file: %v", err)
			}
		}(part)

		if part.FormName() != "file" {
			return false, nil
		}
		_, err = io.Copy(io.Discard, part)
		if err != nil {
			return false, err
		}
		return true, nil
	}

	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Unable to read multipart form data", http.StatusBadRequest)
		log.Printf("Unable to read multipart form data: %+v", err)
		return
	}

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			http.Error(w, "", http.StatusBadRequest)
			return
		}
		ok, err := readPart(part)
		if err != nil {
			http.Error(w, "", http.StatusBadRequest)
			return
		}
		if ok {
			break
		}
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

type ByMethod struct {
	Get  http.HandlerFunc
	Post http.HandlerFunc
}

func routeByMethod(byMethod ByMethod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if byMethod.Get != nil {
				byMethod.Get(w, r)
				return
			}
		case http.MethodPost:
			if byMethod.Post != nil {
				byMethod.Post(w, r)
				return
			}
		default:
		}

		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

type loggerMiddleware struct {
	Handler http.Handler
}

func (l *loggerMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("%v %s %s", time.Now(), r.Method, r.URL)
	l.Handler.ServeHTTP(w, r)
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", routeByMethod(ByMethod{Get: handleRootGet}))
	mux.HandleFunc("/upload", routeByMethod(ByMethod{Post: handleUploadPost}))
	log.Fatal(http.ListenAndServe(":32001", &loggerMiddleware{Handler: mux}))
}
