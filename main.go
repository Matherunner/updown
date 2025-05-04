package main

import (
	"errors"
	"flag"
	"html/template"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"
)

var tmplRoot = template.Must(template.New("Root").Parse(`
<!DOCTYPE html>
<html lang="en">
	<head>
		<meta charset="utf-8">
		<meta name="viewport" content="width=device-width, initial-scale=1">
		<title>Updown</title>
		<style>
			a {
				text-decoration: none;
			}

			a:hover {
				text-decoration: underline;
			}
		</style>
	</head>
	<body>
		<h1>Updown</h1>
		<p>Welcome to updown.</p>
		<form method="post" action="/upload" enctype="multipart/form-data">
			<input type="file" name="file">
			<button type="submit">Upload</button>
		</form>
		<h2>Serving files</h2>
		<p>Path: {{.FullPath}}</p>
		<ul>
		{{range .Files}}
			<li><a href="{{.URL}}">{{.Name}}</a> {{.Type}}</li>
		{{end}}
		</ul>
	</body>
</html>
`))

func getQueryValueOrDefault(query url.Values, key, fallback string) string {
	values := query[key]
	if len(values) == 0 {
		return fallback
	}
	value := values[0]
	if value == "" {
		return fallback
	}
	return value
}

type fileEntry struct {
	URL  string
	Name string
	Type string
}

func addQueryToPath(urlPath string, query map[string]string) (string, error) {
	parsed, err := url.Parse(urlPath)
	if err != nil {
		return "", err
	}
	q := parsed.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

func handleRootGet(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		query := r.URL.Query()
		servePathRoot := getQueryValueOrDefault(query, "p", ".")
		fullPath, err := filepath.Abs(path.Join(*serveDir, servePathRoot))
		if err != nil {
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		entries, err := os.ReadDir(fullPath)
		if err != nil {
			http.Error(w, "", http.StatusInternalServerError)
			return
		}

		entryURL, err := addQueryToPath("/", map[string]string{"p": path.Join(servePathRoot, "..")})
		if err != nil {
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
		fileEntries := []fileEntry{{URL: entryURL, Name: "../", Type: "<DIR>"}}
		for _, entry := range entries {
			var (
				entryType string
				fileName  string
			)
			if entry.IsDir() {
				entryURL, err = addQueryToPath("/", map[string]string{"p": path.Join(servePathRoot, entry.Name())})
				entryType = "<DIR>"
				fileName = entry.Name() + "/"
			} else {
				entryURL, err = addQueryToPath("/download", map[string]string{"p": path.Join(servePathRoot, entry.Name())})
				fileName = entry.Name()
			}
			if err != nil {
				http.Error(w, "", http.StatusInternalServerError)
				return
			}
			fileEntries = append(fileEntries, fileEntry{URL: entryURL, Name: fileName, Type: entryType})
		}

		tmplData := map[string]any{
			"FullPath": fullPath,
			"Files":    fileEntries,
		}

		err = tmplRoot.Execute(w, tmplData)
		if err != nil {
			http.Error(w, "", http.StatusInternalServerError)
			return
		}
	default:
		http.NotFound(w, r)
	}
}

func handleDownloadGet(w http.ResponseWriter, r *http.Request) {
	inputPath := getQueryValueOrDefault(r.URL.Query(), "p", ".")
	fsPath := path.Join(*serveDir, inputPath)
	file, err := os.Open(fsPath)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Printf("Unable to close file: %v", err)
		}
	}(file)
	w.Header().Add("content-type", "application/octet-stream")
	w.Header().Add("content-disposition", "attachment; filename=\""+path.Base(fsPath)+"\"")
	_, err = io.Copy(w, file)
	if err != nil {
		http.Error(w, "", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

		fileName := path.Base(part.FileName())
		outputPath := path.Join(*outputDir, fileName)

		log.Printf("Receive upload of file name %s. Writing to %s.", part.FileName(), outputPath)

		file, err := os.Create(outputPath)
		if err != nil {
			return false, err
		}
		defer func(file *os.File) {
			err := file.Close()
			if err != nil {
				log.Printf("Unable to close file: %v", err)
			}
		}(file)

		_, err = io.Copy(file, part)
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

var (
	outputDir *string
	serveDir  *string
)

func main() {
	portNum := flag.Int("p", 6600, "port number")
	outputDir = flag.String("o", ".", "output directory")
	serveDir = flag.String("s", ".", "directory to serve")
	flag.Parse()

	mux := http.NewServeMux()
	mux.HandleFunc("/", routeByMethod(ByMethod{Get: handleRootGet}))
	mux.HandleFunc("/upload", routeByMethod(ByMethod{Post: handleUploadPost}))
	mux.HandleFunc("/download", routeByMethod(ByMethod{Get: handleDownloadGet}))
	log.Printf("Listening to port %v", *portNum)
	log.Fatal(http.ListenAndServe(":"+strconv.Itoa(*portNum), &loggerMiddleware{Handler: mux}))
}
