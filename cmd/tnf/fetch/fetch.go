package fetch

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/greyerof/oct/internal/registry"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

var (
	filterCertifiedOperators = "&filter=organization==certified-operators"
	containercatalogURL      = "https://catalog.redhat.com/api/containers/v1/images?filter=certified==true"
	containercatalogPageURL  = "https://catalog.redhat.com/api/containers/v1/images?page_size=%d&page=%d&filter=certified==true"
	operatorcatalogURL       = "https://catalog.redhat.com/api/containers/v1/operators/bundles?"
	helmcatalogURL           = "https://charts.openshift.io/index.yaml"
	containersRelativePath   = "%s/cmd/tnf/fetch/data/containers/containers.db"
	operatorsRelativePath    = "%s/cmd/tnf/fetch/data/operators/"
	helmRelativePath         = "%s/cmd/tnf/fetch/data/helm/helm.db"
	certifiedcatalogdata     = "%s/cmd/tnf/fetch/data/archive.json"
	operatorFileFormat       = "operator_catalog_page_%d_%d.db"
)

const (
	containerCatalogPageSize         = 500
	operatorCatalogPageSize          = 500
	catalogPageDownloadTimeoutFactor = 10
)

var (
	command = &cobra.Command{
		Use:   "fetch",
		Short: "fetch the list of certified operators and containers.",
		RunE:  RunCommand,
	}
	operatorFlag  = "operator"
	containerFlag = "container"
	helmFlag      = "helm"
)

type CertifiedCatalog struct {
	Containers int `json:"containers"`
	Operators  int `json:"operators"`
	Charts     int `json:"charts"`
}

func NewCommand() *cobra.Command {
	command.PersistentFlags().BoolP(operatorFlag, "o", false,
		"if specified, the operators DB will be updated")
	command.PersistentFlags().BoolP(containerFlag, "c", false,
		"if specified, the certified containers DB will be updated")
	command.PersistentFlags().BoolP(helmFlag, "m", false,
		"if specified, the helm file will be updated")
	return command
}

// RunCommand execute the fetch subcommands
func RunCommand(cmd *cobra.Command, args []string) error {
	data := getCertifiedCatalogOnDisk()
	log.Info(data)
	b, err := cmd.PersistentFlags().GetBool(operatorFlag)
	if err != nil {
		log.Error("Can't process the flag, ", operatorFlag)
		return err
	} else if b {
		getOperatorCatalog(&data)
	}
	b, err = cmd.PersistentFlags().GetBool(containerFlag)
	if err != nil {
		return err
	} else if b {
		getContainerCatalog(&data)
	}
	b, err = cmd.PersistentFlags().GetBool(helmFlag)
	if err != nil {
		return err
	} else if b {
		getHelmCatalog()
	}
	log.Info(data)
	serializeData(data)
	return nil
}

// getHTTPBody helper function to get binary data from URL
func getHTTPBody(url string) []uint8 {
	//nolint:gosec
	resp, err := http.Get(url)
	if err != nil {
		log.Fatalf("Http request (%s) failed: %s", url, err)
	}

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)

	if err != nil {
		log.Fatalf("Error reading body response from %s: %s, response: %s", url, err, string(body))
	}
	return body
}

func getCertifiedCatalogOnDisk() CertifiedCatalog {
	path, err := os.Getwd()
	if err != nil {
		log.Println(err)
	}
	filePath := fmt.Sprintf(certifiedcatalogdata, path)
	if _, err = os.Stat(filePath); err != nil {
		return CertifiedCatalog{0, 0, 0}
	}
	f, err := os.Open(filePath)
	if err != nil {
		log.Error("can't process file", err, " trying to proceed")
		return CertifiedCatalog{0, 0, 0}
	}
	defer f.Close()
	bytes, err := io.ReadAll(f)
	if err != nil {
		log.Error("can't process file", err, " trying to proceed")
	}
	var data CertifiedCatalog
	if err = yaml.Unmarshal(bytes, &data); err != nil {
		log.Error("error when parsing the data", err)
	}
	return data
}

func serializeData(data CertifiedCatalog) {
	start := time.Now()
	path, err := os.Getwd()
	if err != nil {
		log.Error("can't get current working dir", err)
		return
	}
	filename := fmt.Sprintf(certifiedcatalogdata, path)
	f, err := os.Create(filename)
	if err != nil {
		log.Fatal("Couldn't open file")
	}
	log.Trace("marshall container db into file=", f.Name())
	defer f.Close()
	bytes, _ := json.Marshal(data)
	_, err = f.Write(bytes)
	if err != nil {
		log.Error(err)
	}
	log.Info("serialization time", time.Since(start))
}
func getOperatorCatalogSize() (size, pagesize uint) {
	body := getHTTPBody(fmt.Sprintf("%spage=%d%s", operatorcatalogURL, 0, filterCertifiedOperators))
	var aCatalog registry.OperatorCatalog
	err := json.Unmarshal(body, &aCatalog)
	if err != nil {
		log.Fatalf("Error in unmarshaling body: %v", err)
	}
	return aCatalog.Total, aCatalog.PageSize
}

func getOperatorCatalogPage(page, size uint) {
	path, err := os.Getwd()
	if err != nil {
		log.Fatalf("can't get current working dir: %v", err)
		return
	}
	url := fmt.Sprintf("%spage=%d&page_size=%d%s", operatorcatalogURL, page, size, filterCertifiedOperators)
	log.Infof("Getting operators page with URL: %s", url)
	body := getHTTPBody(url)
	filename := fmt.Sprintf(operatorsRelativePath+"/"+operatorFileFormat, path, page, size)

	f, err := os.Create(filename)
	if err != nil {
		log.Fatalf("couldn't open file: %s", err)
	}
	defer f.Close()
	_, err = f.Write(body)
	if err != nil {
		log.Fatalf("can't write to file %s: %v", filename, err)
	}
}

//nolint:funlen
func getOperatorCatalog(data *CertifiedCatalog) bool {
	start := time.Now()
	total, pageSize := getOperatorCatalogSize()
	if total == uint(data.Operators) {
		log.Info("no new certified operator found")
		return false
	}
	removeOperatorsDB()
	log.Info("we should fetch new data", total, data.Operators)
	pages := total / pageSize
	remaining := total - pages*pageSize

	done := make(chan bool)

	go func(done chan<- bool) {
		var wg sync.WaitGroup
		for page := uint(0); page < pages; page++ {
			wg.Add(1)
			go func(page uint) {
				defer wg.Done()
				getOperatorCatalogPage(page, pageSize)
			}(page)
		}
		if remaining != 0 {
			wg.Add(1)
			go func(page uint) {
				defer wg.Done()
				getOperatorCatalogPage(pages, remaining)
			}(pages)
		}

		wg.Wait()
		done <- true
	}(done)

	operatorsCatalogTout := time.Duration(pages*catalogPageDownloadTimeoutFactor) * time.Second
	log.Infof("Waiting %v secs for all operator pages to be downloaded...", operatorsCatalogTout)
	select {
	case <-done:
		log.Infof("All operator pages retrieved successfully in %s", time.Since(start))
		data.Operators = int(total)
		return true
	case <-time.After(operatorsCatalogTout):
		log.Errorf("Operator pages retrieval timeout.")
		return false
	}
}

func getContainerCatalogSize() (total, pagesize uint) {
	body := getHTTPBody(containercatalogURL)
	var aCatalog registry.ContainerPageCatalog
	err := json.Unmarshal(body, &aCatalog)
	if err != nil {
		log.Fatalf("Error in unmarshaling body: %v", err)
	}
	return aCatalog.Total, aCatalog.PageSize
}

func getContainerCatalogPage(page, size uint, db map[string]*registry.ContainerCatalogEntry) {
	start := time.Now()
	url := fmt.Sprintf(containercatalogPageURL, size, page)
	log.Infof("start fetching data of page %d, url: %s", page, url)
	body := getHTTPBody(url)
	log.Info("time to fetch binary data ", time.Since(start))
	start = time.Now()
	err := registry.LoadBinary(body, db)
	if err != nil {
		log.Fatalf("Failed getting container page %d (size %d): %v", page, size, err)
	}
	log.Info("time to load the data", time.Since(start))
}

func serializeContainersDB(db map[string]*registry.ContainerCatalogEntry) {
	start := time.Now()
	log.Info("start serializing container catalog")
	path, err := os.Getwd()
	if err != nil {
		log.Error("can't get current working dir", err)
		return
	}
	filename := fmt.Sprintf(containersRelativePath, path)
	f, err := os.Create(filename)
	if err != nil {
		log.Fatal("Couldn't open file")
	}
	log.Trace("marshall container db into file=", f.Name())
	defer f.Close()
	bytes, _ := json.Marshal(db)
	_, err = f.Write(bytes)
	if err != nil {
		log.Error(err)
	}
	log.Info("serialization time", time.Since(start))
}

func getContainerCatalog(data *CertifiedCatalog) {
	start := time.Now()
	db := make(map[string]*registry.ContainerCatalogEntry)
	log.Infof("Getting container catalog size (url: %s)", containercatalogURL)
	total, pageSize := getContainerCatalogSize()
	if total == uint(data.Containers) {
		log.Info("no new certified container found")
		return
	}
	removeContainersDB()
	pages := total / pageSize
	remaining := total - pages*pageSize

	log.Infof("Found %d total pages of %d entries each.", total, pageSize)

	maxConcurrentPages := 1
	done := make(chan bool)
	go func(done chan<- bool) {
		var wg sync.WaitGroup
		currConcurrentPages := 0

		for page := uint(0); page < pages; page, currConcurrentPages = page+1, currConcurrentPages+1 {
			if currConcurrentPages > maxConcurrentPages {
				log.Infof("Waiting for %d pages (page %d)", maxConcurrentPages, page)
				currConcurrentPages = 0
				wg.Wait()
				// Wait 5 secs until the next batch of requests
				time.Sleep(time.Duration(5) * time.Second)
			}
			wg.Add(1)
			go func(page uint) {
				defer wg.Done()
				log.Info("getting data from page=", page, (pages - page), " pages to go")
				getContainerCatalogPage(page, pageSize, db)
			}(page)
		}
		if remaining != 0 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				getContainerCatalogPage(pages, remaining, db)
			}()
		}

		wg.Wait()
		done <- true
	}(done)

	containerCatalogTout := time.Duration(pages*catalogPageDownloadTimeoutFactor) * time.Second
	log.Infof("Waiting %v secs for all container pages to be downloaded...", containerCatalogTout)

	select {
	case <-done:
		log.Infof("All container pages retrieved successfully in %s", time.Since(start))
		data.Containers = int(total)
		serializeContainersDB(db)
	case <-time.After(containerCatalogTout):
		log.Fatalf("Container pages retrieval timeout.")
	}
}

func getHelmCatalog() {
	start := time.Now()
	removeHelmDB()
	body := getHTTPBody(helmcatalogURL)
	path, err := os.Getwd()
	if err != nil {
		log.Error("can't get current working dir", err)
		return
	}
	filename := fmt.Sprintf(helmRelativePath, path)
	f, err := os.Create(filename)
	if err != nil {
		log.Fatal("Couldn't open file")
	}
	_, err = f.Write(body)
	if err != nil {
		log.Error(err)
	}
	log.Info("time to process all the charts=", time.Since(start))
}

func removeContainersDB() {
	path, err := os.Getwd()
	if err != nil {
		log.Error("can't get current working dir", err)
		return
	}
	filename := fmt.Sprintf(containersRelativePath, path)
	err = os.Remove(filename)
	if err != nil {
		log.Error("can't remove file", err)
	}
}
func removeHelmDB() {
	path, err := os.Getwd()
	if err != nil {
		log.Error("can't get current working dir", err)
		return
	}
	filename := fmt.Sprintf(helmRelativePath, path)
	err = os.Remove(filename)
	if err != nil {
		log.Error("can't remove file", err)
	}
}
func removeOperatorsDB() {
	path, err := os.Getwd()
	if err != nil {
		log.Println(err)
	}
	path = fmt.Sprintf(operatorsRelativePath, path)
	files, err := os.ReadDir(path)
	if err != nil {
		log.Fatal(err)
	}
	for _, file := range files {
		filePath := fmt.Sprintf("%s/%s", path, file.Name())
		if err = os.Remove(filePath); err != nil {
			log.Error("can't remove file ", filePath)
		}
	}
}
