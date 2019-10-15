package circ

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dereulenspiegel/cripper"
	"github.com/fsnotify/fsnotify"
	"github.com/umahmood/haversine"
)

type Scraper struct {
	client *Client

	scrapeInterval       time.Duration
	TokenRefreshInterval time.Duration

	latTopLeft     float64
	lonTopLeft     float64
	latBottomRight float64
	lonBottomRight float64

	maxAuthRetries int

	phonePrefix string
	phoneNumber string
}

func NewScraper(client *Client, scrapeInterval time.Duration,
	latTopLeft, lonTopLeft, latBottomRight, lonBottomRight float64, phonePrefix, phoneNumber string) *Scraper {
	return &Scraper{
		client:               client,
		scrapeInterval:       scrapeInterval,
		TokenRefreshInterval: DefaultTokenRefreshDuration,
		latTopLeft:           latTopLeft,
		lonTopLeft:           lonTopLeft,
		latBottomRight:       latBottomRight,
		lonBottomRight:       lonBottomRight,
		maxAuthRetries:       5,
		phonePrefix:          phonePrefix,
		phoneNumber:          phoneNumber,
	}
}

func (c *Scraper) Scrape(ctx context.Context, scrapeInterval time.Duration) chan *ScrapeResult {
	out := make(chan *ScrapeResult, 100)
	go func() {
		scrapeTimer := time.NewTimer(scrapeInterval)
		for {
			select {
			case <-ctx.Done():
				close(out)
				return
			case <-scrapeTimer.C:
				scrapeTimer.Stop()
				scooters, err := c.doScrape()
				if err != nil {
					log.Fatalf("Failed to scrape circ finally: %s", err)
				}
				out <- &ScrapeResult{
					Scooters: scooters,
					Date:     time.Now(),
				}
				scrapeTimer = time.NewTimer(c.scrapeInterval)
			}
		}
	}()
	return out
}

func (c *Scraper) doScrape() (scooters []*Scooter, err error) {
	retryCounter := 0
	maxRetries := 5
	authCounter := 0

	success := false
	for ; retryCounter < maxRetries || !success; retryCounter = retryCounter + 1 {
		if scooters, err = c.client.Scooters(c.latTopLeft, c.lonTopLeft, c.latBottomRight, c.lonBottomRight); err != nil {
			if circErr, ok := err.(CircError); ok {
				if circErr.Status >= 400 && circErr.Status < 500 {

					for ; authCounter < c.maxAuthRetries; authCounter = authCounter + 1 {
						err := c.client.Login(c.phonePrefix, c.phoneNumber, func() string {
							fmt.Print("Please enter SMS code: ")
							reader := bufio.NewReader(os.Stdin)
							code, _ := reader.ReadString('\n')
							code = strings.Replace(code, "\n", "", -1)
							fmt.Println("Thank you")
							return code
						})
						if err == nil {
							break
						}
					}
					if authCounter >= c.maxAuthRetries {
						log.Fatalf("Failed to authenticate with Circ")
					}
				} else {
					log.Fatalf("Unhandable error from Circ: %s", circErr.Error())
				}
			} else {
				retryCounter++

				if retryCounter == maxRetries {
					log.Fatalf("Failed to retrieve scooters with unknown error: %s", err)
				} else {
					log.Printf("Failed to retrieve scooters with unknown error, retrying: %s", err)
				}
				time.Sleep(time.Second * 5)
				continue
			}
		} else {
			success = true

		}
	}
	return
}

type ScrapeResult struct {
	Date     time.Time
	Scooters []*Scooter
}

func (c *ScrapeResult) ScrapeDate() time.Time {
	return c.Date
}

func (c *ScrapeResult) Provider() string {
	return "circ"
}

func (c *ScrapeResult) Content() []byte {
	data, _ := json.Marshal(c.Scooters)
	return data
}

func SplitChan(in chan *ScrapeResult) (chan *ScrapeResult, chan *ScrapeResult) {
	out1 := make(chan *ScrapeResult, 100)
	out2 := make(chan *ScrapeResult, 100)
	go func() {
		for res := range in {
			out1 <- res
			out2 <- res
		}
		close(out1)
		close(out2)
	}()
	return out1, out2
}

type TripAggregator struct {
	unfinishedTrips map[string]*cripper.Trip
	lastScooters    Scooters
}

func NewTripAggregator() *TripAggregator {
	return &TripAggregator{
		unfinishedTrips: make(map[string]*cripper.Trip),
		lastScooters:    NewScooters([]*Scooter{}),
	}
}

func (c *TripAggregator) Aggregate(in chan *ScrapeResult) <-chan *cripper.Trip {
	out := make(chan *cripper.Trip, 100)
	go func() {
		for res := range in {
			scooters := NewScooters(res.Scooters)
			vanishedScooter := scooters.difference(c.lastScooters)
			for id, scooter := range vanishedScooter {
				trip := &cripper.Trip{
					ScooterID:        id,
					ScooterProvider:  "circ",
					StartChargeLevel: float64(scooter.EnergyLevel),
					StartLocation:    cripper.NewGeoLocation(scooter.Latitude, scooter.Longitude),
					StartTime:        res.ScrapeDate(),
				}
				c.unfinishedTrips[id] = trip
			}

			for id, trip := range c.unfinishedTrips {
				if scooter, exists := scooters[id]; exists {
					trip.EndChargeLevel = float64(scooter.EnergyLevel)
					trip.EndLocation = cripper.NewGeoLocation(scooter.Latitude, scooter.Longitude)
					trip.UserID = scooter.StateUpdatedByUserIdentifier
					trip.EndTime = res.ScrapeDate()
					trip.Duration = trip.EndTime.Sub(trip.StartTime)
					trip.Cost = uint64(scooter.InitPrice + (scooter.Price * int(trip.Duration.Minutes())))

					_, distanceKm := haversine.Distance(
						haversine.Coord{Lat: trip.StartLocation.Latitude, Lon: trip.StartLocation.Longitude},
						haversine.Coord{Lat: trip.EndLocation.Latitude, Lon: trip.EndLocation.Longitude},
					)
					trip.Distance = distanceKm

					delete(c.unfinishedTrips, id)
					out <- trip
				}
			}
		}
	}()
	return out
}

type Scooters map[string]*Scooter

func NewScooters(in []*Scooter) Scooters {
	s := make(Scooters)
	for _, scooter := range in {
		s[scooter.Identifier] = scooter
	}
	return s
}

func (s Scooters) difference(ns Scooters) Scooters {
	s2 := make(Scooters)

	for id, scooter := range ns {
		if _, exists := s[id]; !exists {
			s2[id] = scooter
		}
	}
	return s2
}

type FileScraper struct {
	baseDir string

	fileNameChan           chan string
	fileWatcher            *fsnotify.Watcher
	folderWatcher          *fsnotify.Watcher
	currentlyWatchedFolder string
	watchMutex             *sync.Mutex
}

func NewFileScraper(baseDir string) *FileScraper {
	return &FileScraper{
		baseDir:      baseDir,
		fileNameChan: make(chan string, 10000),
		watchMutex:   &sync.Mutex{},
	}
}

func (c *FileScraper) Scrape(ctx context.Context) (<-chan *ScrapeResult, error) {
	var subfolderNames []string

	subFiles, err := ioutil.ReadDir(c.baseDir)
	if err != nil {
		return nil, err
	}
	for _, f := range subFiles {
		if f.IsDir() {
			subfolderNames = append(subfolderNames, filepath.Join(c.baseDir, f.Name()))
		}
	}
	sort.Strings(subfolderNames)
	c.fileWatcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	c.folderWatcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	out := make(chan *ScrapeResult, 1000)
	fileBufferChan := make(chan *ScrapeResult, 1000)
	buffering := true
	err = c.folderWatcher.Add(c.baseDir)
	if err != nil {
		return nil, err
	}
	folderCtx, folderCancel := context.WithCancel(ctx)
	go func() {
		defer folderCancel()
		for {
			select {
			case <-folderCtx.Done():
				return
			case evt := <-c.folderWatcher.Events:
				if evt.Op == fsnotify.Create {
					c.watchMutex.Lock()
					c.fileWatcher.Remove(c.currentlyWatchedFolder)
					newFolder := filepath.Join(c.baseDir, evt.Name)
					c.currentlyWatchedFolder = newFolder
					err := c.fileWatcher.Add(newFolder)
					if err != nil {
						log.Fatalf("[ERROR] Failed to watch folder %s: %s", newFolder, err)
					}
					c.watchMutex.Unlock()
				}
			}
		}

	}()
	err = c.fileWatcher.Add(subfolderNames[len(subfolderNames)-1])
	if err != nil {
		return nil, err
	}
	fileCtx, fileCancel := context.WithCancel(ctx)
	go func() {
		defer fileCancel()
		for {
			select {
			case <-fileCtx.Done():
				close(out)
				return
			case evt := <-c.fileWatcher.Events:
				if evt.Op == fsnotify.Create {
					scrapeFilePath := filepath.Join(c.baseDir, c.currentlyWatchedFolder, evt.Name)
					res, err := c.handleNewFile(scrapeFilePath)
					if err != nil {
						log.Printf("[ERROR]: Failed to process created file %s: %s", evt.Name, err)
						continue
					}
					c.watchMutex.Lock()
					if buffering {
						fileBufferChan <- res
					} else {
						out <- res
					}
					c.watchMutex.Lock()
				}
			}
		}
	}()

	go func() {
		for _, subFolder := range subfolderNames {
			subFilesInfos, err := ioutil.ReadDir(subFolder)
			folderPath := filepath.Join(c.baseDir, subFolder)
			if err != nil {
				log.Fatalf("[ERROR] Failed to read directory %s: %s", folderPath, err)
				continue
			}

			for _, scrapeFile := range subFilesInfos {
				circFilePath := filepath.Join(folderPath, scrapeFile.Name())
				res, err := c.handleNewFile(circFilePath)
				if err != nil {
					log.Fatalf("[ERROR] Failed to process file %s: %s", circFilePath, err)
					continue
				}
				out <- res
			}
		}
		for res := range fileBufferChan {
			out <- res
		}
		c.watchMutex.Lock()
		buffering = false
		c.watchMutex.Unlock()
		close(fileBufferChan)
	}()

	return out, nil
}

func (c *FileScraper) handleNewFile(path string) (*ScrapeResult, error) {
	scrapeFileName := filepath.Base(path)
	scrapeFile, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer scrapeFile.Close()
	fileDate, err := extractDateFromFilename(scrapeFileName)
	if err != nil {
		return nil, err
	}

	res := &ScrapeResult{
		Date:     fileDate,
		Scooters: []*Scooter{},
	}
	gzipReader, err := gzip.NewReader(scrapeFile)
	if err != nil {
		return nil, err
	}
	defer gzipReader.Close()
	if err = json.NewDecoder(gzipReader).Decode(&res.Scooters); err != nil {
		return nil, err
	}
	return res, nil
}

var (
	fileNameRegex = regexp.MustCompile(`^circ_([0-9-T:+]+).json.gz$`)
)

func extractDateFromFilename(fileName string) (time.Time, error) {
	matches := fileNameRegex.FindAllStringSubmatch(fileName, -1)

	stringDate := matches[0][1]

	return time.Parse(time.RFC3339, stringDate)
}
