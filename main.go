//usr/bin/env go run "$0" "$@"; exit "$?"
package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Testsuite struct {
	XMLName   xml.Name   `xml:"testsuite"`
	Testcases []Testcase `xml:"testcase"`
	Name      string     `xml:"name,attr"`
	Tests     int        `xml:"tests,attr"`
	Failures  int        `xml:"failures,attr"`
	Errors    int        `xml:"errors,attr"`
	Skipped   int        `xml:"skipped,attr"`
	Time      string     `xml:"time,attr"`
	Timestamp string     `xml:"timestamp,attr"`
	Hostname  string     `xml:"hostname,attr"`
}

type Testcase struct {
	XMLName   xml.Name `xml:"testcase"`
	Classname string   `xml:"classname,attr"`
	Name      string   `xml:"name,attr"`
	Time      int      `xml:"time,attr"`
	Failure   Failure  `xml:"failure"`
}

type Failure struct {
	XMLName xml.Name `xml:"failure"`
	Type    string   `xml:"type,attr"`
	Message string   `xml:",chardata"`
}

func getFiles(args []string) ([]string, error) {
	if len(args) == 0 {
		return getFilesFromPath("./")
	}

	var files []string
	for _, arg := range args {
		f, err := os.Stat(arg)
		if err != nil {
			return files, err
		}
		if f.IsDir() {
			filesInPath, err := getFilesFromPath(arg)
			if err != nil {
				return files, err
			}

			for _, file := range filesInPath {
				if filepath.Ext(file) == ".xml" {
					files = append(files, file)
				}
			}
		} else {
			if filepath.Ext(f.Name()) == ".xml" {
				files = append(files, f.Name())
			}
		}
	}

	return files, nil
}

func getFilesFromPath(path string) ([]string, error) {
	path = strings.TrimSuffix(path, "/")
	var files []string
	filePaths, err := ioutil.ReadDir(path)
	if err != nil {
		return files, err
	}

	for _, f := range filePaths {
		if f.IsDir() {
			continue
		}
		if filepath.Ext(f.Name()) == ".xml" {
			files = append(files, fmt.Sprintf("%s/%s", path, f.Name()))
		}
	}

	return files, nil
}

func processFile(file string, skipOk bool) (string, error) {
	body := ""

	xmlFile, err := os.Open(file)
	if err != nil {
		return body, err
	}

	defer xmlFile.Close()

	byteValue, _ := ioutil.ReadAll(xmlFile)
	var testsuite Testsuite
	xml.Unmarshal(byteValue, &testsuite)

	if !skipOk || testsuite.Failures > 0 {
		message := fmt.Sprintf("1..%d (%s)", testsuite.Tests, testsuite.Name)
		body += "### " + message + "\n\n"
		println(message)
	}

	for i, testcase := range testsuite.Testcases {
		if len(testcase.Failure.Message) == 0 {
			if !skipOk {
				message := fmt.Sprintf("ok %d %s in %dsec", i, testcase.Name, testcase.Time)
				body += "<details><summary>" + message + "</summary></details>\n"
				println(message)
			}
		} else {
			message := fmt.Sprintf("not ok %d %s in %dsec", i, testcase.Name, testcase.Time)
			body += "<details><summary>" + message + "</summary>\n"
			println(message)
			lines := strings.Split("\n"+strings.TrimSpace(testcase.Failure.Message)+"\n", "\n")
			for _, line := range lines {
				message := fmt.Sprintf("    %v", line)
				body += message + "\n"
				println(message)
			}
			body += "</details>\n"
		}
	}

	return body, nil
}

func main() {
	flags := flag.NewFlagSet("xunit-to-github", flag.ExitOnError)
	skipOk := flags.Bool("skip-ok", false, "skip-ok: Whether to skip ok tests or not")
	title := flags.String("title", "", "title: A title for the comment")
	jobUrl := flags.String("job-url", "", "job-url: A url for the report")
	pullRequestId := flags.Int("pull-request-id", 0, "pull-request-id: A pull request ID")
	repositorySlug := flags.String("repository-slug", "", "repository-slug: The slug of the repository")
	flags.Parse(os.Args[1:])
	args := flags.Args()

	files, err := getFiles(args)
	if err != nil {
		log.Fatal(err)
	}

	body := ""
	for _, file := range files {
		data, err := processFile(file, *skipOk)
		if err != nil {
			log.Fatal(err)
		}
		body += data + "\n"
	}

	githubAccessToken := os.Getenv("GITHUB_ACCESS_TOKEN")
	if githubAccessToken == "" {
		return
	}

	if *pullRequestId == 0 || *repositorySlug == "" {
		return
	}

	if body == "" {
		return
	}

	if *jobUrl != "" {
		body = fmt.Sprintf("[Build Url](%s)", *jobUrl) + "\n\n" + body
	}

	if *title != "" {
		body = "## " + *title + "\n\n" + body
	}

	message := map[string]interface{}{
		"body": body,
	}

	data, err := json.Marshal(message)
	if err != nil {
		log.Fatal(err)
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/issues/%d/comments", *repositorySlug, *pullRequestId)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(data))
	req.Header.Set("Authorization", "token "+githubAccessToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 201 {
		fmt.Println("Comment posted to github")
		return
	}

	responseBody, _ := ioutil.ReadAll(resp.Body)
	log.Fatal("err:", string(responseBody))
}
