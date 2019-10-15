package main

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dereulenspiegel/sharealyzer/circ"
)

var (
	phonePrefix    = flag.String("phonePrefix", "+49", "Country prefix of your phone number in + format")
	phoneNumber    = flag.String("phoneNumber", "", "Your phone number to authenticate")
	tokenStorePath = flag.String("tokenPath", "./.tokens", "The path where to persist tokens")
	latTopLeft     = flag.Float64("latTopLef", 51.582780, "Latitude Top Left")
	lonTopLeft     = flag.Float64("lonTopLeft", 7.325945, "Longitude Top Left")
	latBottomRight = flag.Float64("larBottomLeft", 51.475727, "Latitude Bottom Left")
	lonBottomRight = flag.Float64("lonBottomRight", 7.558172, "Longitude Bottom right")

	expectedZone   = flag.String("zone", "", "Only accept scooters from the specified zone")
	outPath        = flag.String("out", "./out", "Directory where to put scrape results")
	scrapeInterval = flag.Duration("interval", time.Minute*1, "Scrape Interval")

	authCounter  = 0
	maxAuthTries = 3
)

func main() {
	flag.Parse()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ctx := context.Background()
	scrapeCtx, scrapeCancel := context.WithCancel(ctx)

	go func() {

		tokenStore := &circ.FileTokenStore{*tokenStorePath}
		cc := circ.New(circ.WithTokenStore(tokenStore))

		go scrape(scrapeCtx, cc)
	}()

	select {
	case sig := <-sigs:
		log.Printf("Exiting due to signal %s", sig.String())
		scrapeCancel()
		os.Exit(0)
	}
}

const folderTimeFormat = "2006-01-02"

func writeResult(scooters []*circ.Scooter) {
	if *expectedZone != "" {
		filteredScooters := make([]*circ.Scooter, 0, len(scooters))
		for _, s := range scooters {
			if s.ZoneIdentifier == *expectedZone {
				filteredScooters = append(filteredScooters, s)
			}
		}
		scooters = filteredScooters
	}

	timestamp := time.Now().Format(time.RFC3339)
	folderName := fmt.Sprintf("circ_%s", time.Now().Format(folderTimeFormat))
	fileName := fmt.Sprintf("circ_%s.json.gz", timestamp)
	folderPath := filepath.Join(*outPath, folderName)

	if !fileDoesExist(folderPath) {
		if err := os.MkdirAll(folderPath, 0770); err != nil {
			log.Fatalf("Failed to create output folder(%s): %s", folderPath, err)
		}
	}
	path := filepath.Join(folderPath, fileName)

	outFile, err := os.Create(path)
	if err != nil {
		log.Fatalf("Failed to create output file %s: %s", path, err)
		return
	}
	defer outFile.Close()
	gzipWriter, err := gzip.NewWriterLevel(outFile, gzip.BestCompression)
	if err != nil {
		log.Fatalf("Failed to create GZIP writer: %s", err)
	}
	defer gzipWriter.Close()
	if err := json.NewEncoder(gzipWriter).Encode(scooters); err != nil {
		log.Fatalf("Failed to serialize scooter to %s: %s", path, err)
	}
}

func scrape(ctx context.Context, cc *circ.Client) {
	scrapeTimer := time.NewTimer(*scrapeInterval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-scrapeTimer.C:
			scrapeTimer.Stop()
			doScrape(cc)
			scrapeTimer = time.NewTimer(*scrapeInterval)
		}
	}
}

func doScrape(cc *circ.Client) {
	retryCounter := 0
	maxRetries := 5

	success := false
	for ; retryCounter < maxRetries || !success; retryCounter = retryCounter + 1 {
		if scooters, err := cc.Scooters(*latTopLeft, *lonTopLeft, *latBottomRight, *lonBottomRight); err != nil {
			if circErr, ok := err.(circ.CircError); ok {
				if circErr.Status >= 400 && circErr.Status < 500 {

					for ; authCounter < maxAuthTries; authCounter = authCounter + 1 {
						err := cc.Login(*phonePrefix, *phoneNumber, func() string {
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
					if authCounter >= maxAuthTries {
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
			writeResult(scooters)
		}
	}

}

func fileDoesExist(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	} else if os.IsExist(err) {
		return true
	} else if err == nil {
		return true
	} else {
		return false
	}
}
