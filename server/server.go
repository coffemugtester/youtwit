package server

import (
	"html/template"
	"log"
	"net/http"

	"github.com/spf13/cobra"
)

var tpl = template.Must(template.New("index").Parse(`
<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>Hello</title></head>
<body>
  <h1>Hello, {{.Name}}!</h1>
	<p> Test {{.Summary}} </p>
</body>
</html>`))

func handler(w http.ResponseWriter, r *http.Request) {
	_ = tpl.Execute(w, struct{ Name string; Summary string }{Name: "world", Summary: "The description"})
}

func Start(command *cobra.Command, args []string) {
	http.HandleFunc("/", handler)
	log.Println("listening on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

