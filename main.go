package main

import (
	"flag"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"runtime"
	"bytes"
	"time"

	"github.com/juju/errors"
	"golang.org/x/net/html"
)

var (
	StartURL = flag.String("start", "", "page to start the scan")

	Requests []*Request
	Visited = map[string]bool{}
	Ignored = map[string]bool{}

	NotFoundErrors []*NotFoundErr
)

type Request struct {
	URL  string
	From string
}

type NotFoundErr struct {
	URL  string
	From string
}

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())
	flag.Parse()

	protect(run)
}

func protect(fn func() error) {
	if err := fn(); err != nil {
		log.Fatal(errors.ErrorStack(err))
	}
}

func run() error {
	if *StartURL == "" {
		return errors.New("start is required")
	}

	Requests = append(Requests, &Request{
		URL:  *StartURL,
		From: "command line",
	})

	if err := requester(); err != nil {
	  return errors.Trace(err)
	}

	log.Println(" ============================================================= ")
	log.Println("    OVERVIEW OF NOT FOUND ERRORS")
	log.Println(" ============================================================= ")
	for _, e := range NotFoundErrors {
		log.Printf("from <%s>: %s", e.From, e.URL)
	}
	log.Println(" ============================================================= ")

	return nil
}

func requester() error {
	for i := 0; i < len(Requests); i++ {
		log.Printf("request [%d / %d]: <%s>\n", i, len(Requests), Requests[i].URL)
		if err := request(Requests[i]); err != nil {
			return errors.Trace(err)
		}

		time.Sleep(1 * time.Second)
	}

	return nil
}

func request(req *Request) error {

	client := &http.Client{}
	resp, err := client.Get(req.URL)
	if err != nil {
		return errors.Trace(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		NotFoundErrors = append(NotFoundErrors, &NotFoundErr{
			URL:  req.URL,
			From: req.From,
		})

		log.Printf("found 404 error from <%s>: <%s>\n", req.From, req.URL)

		return nil
	}

	if resp.StatusCode != 200 {
		return errors.Errorf("http error: %d: %s", resp.StatusCode, req.URL)
	}

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return errors.Trace(err)
	}

	log.Println("process response")

	doc, err := html.Parse(bytes.NewReader(content))
	if err != nil {
	  return errors.Trace(err)
	}

	base, err := url.Parse(req.URL)
	if err != nil {
	  return errors.Trace(err)
	}

	Visited[base.Path] = true

	var process func(*html.Node) error
	process = func(n *html.Node) error {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, attr := range n.Attr {
				if attr.Key == "href" {
					u, err := url.Parse(attr.Val)
					if err != nil {
					  return errors.Trace(err)
					}

					if u.IsAbs() {
						if u.Scheme != "http" && u.Scheme != "https" {
							if !Ignored[u.Scheme] {
								log.Printf("ignoring link with unknown scheme <%s>\n", u.Scheme)
							}
							
							Ignored[u.Scheme] = true
						} else if u.Host != base.Host {
							if !Ignored[u.Host] {
								log.Printf("ignoring link with external host <%s>\n", u.Host)
							}
							
							Ignored[u.Host] = true
						} else if !Visited[u.Path] {
							Requests = append(Requests, &Request{
								URL: base.String(),
								From: req.URL,
							})
							
							Visited[u.Path] = true
						}
					} else {
						resolved := base.ResolveReference(u)
						if !Visited[resolved.Path] {
							Requests = append(Requests, &Request{
								URL: resolved.String(),
								From: req.URL,
							})

							Visited[resolved.Path] = true
						}
					}

					break
				}
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := process(c); err != nil {
			  return errors.Trace(err)
			}
		}

		return nil
	}
	if err := process(doc); err != nil {
	  return errors.Trace(err)
	}

	return nil
}
