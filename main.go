package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"errors"

	"github.com/karen-irc/popuko/queue"
	"github.com/karen-irc/popuko/setting"
)

var config *setting.Settings

var (
	revision  string
	builddate string
)

func main() {
	var configDir string
	{
		c := "Specify the config dir as absolute path. default: $" + setting.XdgConfigHomeEnvKey + "/" + setting.HomeDirName
		flag.StringVar(&configDir, "config-base-dir", "", c)
	}
	var useTLS bool
	{
		c := "whether the server uses TLS (https://) or not (default: false)"
		flag.BoolVar(&useTLS, "tls", false, c)
	}
	var certFile string
	{
		c := "Specify the absolute path to the cert file. (default: none)"
		flag.StringVar(&certFile, "cert", "", c)
	}
	var keyFile string
	{
		c := "Specify the absolute path to the key file. (default: none)"
		flag.StringVar(&keyFile, "key", "", c)
	}
	flag.Parse()

	ok, root := setting.HomeDir(configDir)
	if !ok {
		log.Println("info: cannot find the config dir fot this.")
		return
	}

	// Check whether the dir exists. If there is none, create it.
	if _, err := os.Stat(root); err != nil {
		if err := os.MkdirAll(root, os.ModePerm); err != nil {
			log.Println("error: cannot create the config home dir.")
			return
		}
	}

	var certPath string
	var keyPath string
	if useTLS {
		var err error
		certPath, err = checkPath(certFile)
		if err != nil {
			log.Printf("error: use TLS, but `--cert` is invalid path: %v\n", err)
			return
		}

		keyPath, err = checkPath(keyFile)
		if err != nil {
			log.Printf("error: use TLS, but `--key` is invalid path: %v\n", err)
			return
		}
	}

	config = setting.LoadSettings(root)
	if config == nil {
		log.Println("Cannot find $XDG_CONFIG_HOME/popuko" + setting.RootConfigFile)
		return
	}

	log.Println("===== popuko =====")
	log.Printf("version (git revision): %s\n", revision)
	log.Printf("builddate: %s\n", builddate)
	log.Printf("use TLS: %v\n", useTLS)
	if useTLS {
		log.Printf("cert: %v\n", certPath)
		log.Printf("key: %v\n", keyPath)
	}
	log.Printf("listen http on port: %v\n", config.PortStr())
	log.Printf("botname for GitHub: %v\n", "@"+config.BotNameForGithub())
	log.Printf("config dir: %v\n", root)
	log.Println("==================")

	github := createGithubClient(config)
	if github == nil {
		log.Println("error: Cannot create the github client")
		return
	}

	q := queue.NewAutoMergeQRepo(root)
	if q == nil {
		log.Println("Fail to initialize the merge queue")
		return
	}

	server := AppServer{
		githubClient:  github,
		autoMergeRepo: q,
		setting:       config,
	}

	http.HandleFunc(prefixWebHookPath, server.handleGithubHook)
	http.HandleFunc("/", server.handleRESTApiRequest)

	if useTLS {
		http.ListenAndServeTLS(config.PortStr(), certPath, keyPath, nil)
	} else {
		http.ListenAndServe(config.PortStr(), nil)
	}
}

func checkPath(path string) (fullpath string, err error) {
	if path == "" {
		return "", errors.New("Not empty string")
	}

	p, err := filepath.Abs(path)
	if err != nil {
		return "", errors.New("Fail to parse the path")
	}

	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("Not exist the file: %v", p)
	}

	return p, nil
}
