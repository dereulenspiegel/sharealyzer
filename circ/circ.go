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
	"github.com/pkg/errors"
	"github.com/umahmood/haversine"
)

// Scraper uses a circ client to scrape a region for available circ scooters
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

// NewScraper creates a new Scraper with the the given Client. It lets you specify
// a rectangle of geo coordinates. phonePrefix and phoneNumber are necessary for authentication.
func NewScraper(client *Client,
	latTopLeft, lonTopLeft, latBottomRight, lonBottomRight float64, phonePrefix, phoneNumber string) *Scraper {
	return &Scraper{
		client:               client,
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

// Scrape starts the scraping process with the specified interval and returns a channel with items containing
// the scrape date and all scraped scooters
func (c *Scraper) Scrape(ctx context.Context, scrapeInterval time.Duration) <-chan *ScrapeResult {
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

// ScrapeResult contains all scraped scooters with the date when these scooters were scraped from the API
type ScrapeResult struct {
	Date     time.Time
	Scooters []*Scooter
}

// ScrapeDate returns the date when this ScrapeResult was created
func (c *ScrapeResult) ScrapeDate() time.Time {
	return c.Date
}

// Provider returns from which provider this was scraped, here it is always circ
func (c *ScrapeResult) Provider() string {
	return "circ"
}

// Content returns the serialized content, in this case the slice of scraped scooters
func (c *ScrapeResult) Content() []byte {
	data, _ := json.Marshal(c.Scooters)
	return data
}

// SplitChan splits a channel of ScrapeResults so these results can be used in two different process like
// storage and aggregation
func SplitChan(in <-chan *ScrapeResult) (<-chan *ScrapeResult, <-chan *ScrapeResult) {
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

// TripAggregator tries to aggregate ScrapeResults to Trips
type TripAggregator struct {
	unfinishedTrips map[string]*cripper.Trip
	lastScooters    Scooters

	debug bool
}

// NewTripAggregator creates a new TripAggregator
func NewTripAggregator() *TripAggregator {
	return &TripAggregator{
		unfinishedTrips: make(map[string]*cripper.Trip),
		lastScooters:    NewScooters([]*Scooter{}),
		debug:           false,
	}
}

// Aggregate takes a channel of ScrapeResult and returns a channel of aggregated Trips
func (c *TripAggregator) Aggregate(in <-chan *ScrapeResult) <-chan *cripper.Trip {
	out := make(chan *cripper.Trip, 100)
	go func() {
		for res := range in {
			if c.debug {
				log.Printf("Received scrape result from %s", res.ScrapeDate().Format(time.RFC3339))
			}
			scooters := NewScooters(res.Scooters)
			vanishedScooter := scooters.Difference(c.lastScooters)
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

// Scooters is a map of Scooters in a ScrapeResult. This makes it easier to create differences
// from other sets of Scooters and to look up Scooters
type Scooters map[string]*Scooter

// NewScooters creates a new map based Scooters type from a slice of Scooters
func NewScooters(in []*Scooter) Scooters {
	s := make(Scooters)
	for _, scooter := range in {
		s[scooter.Identifier] = scooter
	}
	return s
}

// Difference returns all scooters which exist in ns but not in this Scooters instance
func (s Scooters) Difference(ns Scooters) Scooters {
	s2 := make(Scooters)

	for id, scooter := range ns {
		if _, exists := s[id]; !exists {
			s2[id] = scooter
		}
	}
	return s2
}

// FileScraper uses a folder structure as input to generate a channel of ScrapeResults.
// It also watches for new subfolders and files and feeds them to the channel.
type FileScraper struct {
	baseDir string

	fileNameChan           chan string
	fileWatcher            *fsnotify.Watcher
	folderWatcher          *fsnotify.Watcher
	currentlyWatchedFolder string
	watchMutex             *sync.Mutex

	debug bool
}

// NewFileScraper creates a new FileScraper scraping the given baseDir
func NewFileScraper(baseDir string) *FileScraper {
	return &FileScraper{
		baseDir:      baseDir,
		fileNameChan: make(chan string, 10000),
		watchMutex:   &sync.Mutex{},
		debug:        false,
	}
}

// Scrape actually starts the scraping process. This means reading all existing files and then
// watching for new files.
func (c *FileScraper) Scrape(ctx context.Context) (<-chan *ScrapeResult, error) {
	var subfolderNames []string

	subFiles, err := ioutil.ReadDir(c.baseDir)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to read base directory")
	}
	for _, f := range subFiles {
		if f.IsDir() {
			subfolderNames = append(subfolderNames, filepath.Join(c.baseDir, f.Name()))
		}
	}
	sort.Strings(subfolderNames)
	c.fileWatcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to create file watcher")
	}
	c.folderWatcher, err = fsnotify.NewWatcher()
	if err != nil {
		return nil, errors.Wrap(err, "Failed to create folder watcher")
	}

	out := make(chan *ScrapeResult, 1000)
	fileBufferChan := make(chan *ScrapeResult, 1000)
	buffering := true
	err = c.folderWatcher.Add(c.baseDir)
	if err != nil {
		return nil, errors.Wrap(err, "Failed to watch base dir")
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
	c.currentlyWatchedFolder = subfolderNames[len(subfolderNames)-1]
	err = c.fileWatcher.Add(c.currentlyWatchedFolder)
	if err != nil {
		return nil, errors.Wrapf(err, "Failed to watch latest sub folder %s", c.currentlyWatchedFolder)
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
					// FIXME this file is not really finished writing, we need to fix this
					scrapeFilePath := evt.Name
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
			if err != nil {
				log.Fatalf("[ERROR] Failed to read directory %s: %s", subFolder, err)
				continue
			}
			scrapeFileNames := make([]string, 0, len(subFilesInfos))
			for _, subInfo := range subFilesInfos {
				scrapeFileNames = append(scrapeFileNames, filepath.Join(subFolder, subInfo.Name()))
			}
			sort.Strings(scrapeFileNames)

			for _, scrapeFile := range scrapeFileNames {
				circFilePath := scrapeFile
				res, err := c.handleNewFile(circFilePath)
				if err != nil {
					log.Fatalf("[ERROR] Failed to process file %s: %s", circFilePath, err)
					continue
				}
				out <- res
			}
		}
		c.watchMutex.Lock()
		buffering = false
		close(fileBufferChan)
		// drain the buffer
		for res := range fileBufferChan {
			out <- res
		}
		c.watchMutex.Unlock()

	}()

	return out, nil
}

func (c *FileScraper) handleNewFile(path string) (*ScrapeResult, error) {
	if c.debug {
		log.Printf("Processing file %s", path)
	}
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
