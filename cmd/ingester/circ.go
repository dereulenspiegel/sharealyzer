package main

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/dereulenspiegel/cripper/circ"
)

const folderTimeFormat = "2006-01-02"

var (
	fileNameRegex = regexp.MustCompile(`^circ_([0-9-T:+]+).json.gz$`)
)

type CircAggregator struct {
	baseDir string

	state *AggregatorState
}

func NewCircAggregator(baseDir string) *CircAggregator {
	return &CircAggregator{
		baseDir: baseDir,
		state:   &AggregatorState{},
	}
}

type AggregatorState struct {
	CurrTime   time.Time
	Files      []string
	FileCursor int
}

func (c *CircAggregator) listDayFiles(date time.Time) (circFiles []string, err error) {
	dayFolderName := fmt.Sprintf("circ_%s", date.Format(folderTimeFormat))
	fileInfos, err := ioutil.ReadDir(filepath.Join(c.baseDir, dayFolderName))
	if err != nil {
		return nil, err
	}
	for _, f := range fileInfos {
		circFiles = append(circFiles, filepath.Join(dayFolderName, f.Name()))
	}
	return
}

func extractDateFromFilename(fileName string) (time.Time, error) {
	matches := fileNameRegex.FindAllStringSubmatch(fileName, -1)

	stringDate := matches[0][1]

	return time.Parse(time.RFC3339, stringDate)
}

func (c *CircAggregator) nextFile() (scooters []*circ.Scooter, fileTime time.Time, err error) {
	if len(c.state.Files) == 0 {
		c.state.Files, err = c.listDayFiles(c.state.CurrTime)
		if err != nil {
			return
		}
		c.state.FileCursor = 0
	}
	defer func() {
		c.state.FileCursor = c.state.FileCursor + 1
	}()

	if len(c.state.Files) == c.state.FileCursor {
		c.state.CurrTime = c.state.CurrTime.Add(time.Hour * 23)
		c.state.Files, err = c.listDayFiles(c.state.CurrTime)
		if err != nil {
			return
		}
		c.state.FileCursor = 0
	}

	if len(c.state.Files) == 0 {
		// Probably no more files to read
		err = errors.New("No new files")
		return
	}

	scooterFileName := c.state.Files[c.state.FileCursor]

	//log.Printf("Processing file %s", scooterFileName)

	baseName := filepath.Base(scooterFileName)
	fileTime, err = extractDateFromFilename(baseName)
	if err != nil {
		return
	}
	c.state.CurrTime = fileTime

	filePath := filepath.Join(c.baseDir, scooterFileName)

	scooterFile, err := os.Open(filePath)
	if err != nil {
		return
	}

	gzipReader, err := gzip.NewReader(scooterFile)
	if err != nil {
		return
	}
	scooters = []*circ.Scooter{}
	err = json.NewDecoder(gzipReader).Decode(&scooters)
	return
}

func (c *CircAggregator) Aggregate(from, to time.Time, aggr func(fileDate time.Time, scooters []*circ.Scooter) error) (err error) {
	c.state.CurrTime = from
	c.state.Files = []string{}
	c.state.FileCursor = 0

	var currentFileDate time.Time
	var currentScooters []*circ.Scooter

	for c.state.CurrTime.Before(to) && err == nil {
		currentScooters, currentFileDate, err = c.nextFile()
		if err != nil {
			log.Printf("Breaking because of error: %s", err)
			break
		}
		if err = aggr(currentFileDate, currentScooters); err != nil {
			break
		}
	}
	return
}

func (c *CircAggregator) AggregateUniqueScooters(from, to time.Time) ([]string, error) {
	c.state.CurrTime = from

	uniqueIDs := make(map[string]bool)

	var err error
	for c.state.CurrTime.Before(to) && err == nil {
		s, _, err := c.nextFile()
		if err != nil {
			break
		}
		for _, scooter := range s {
			if !uniqueIDs[scooter.Identifier] {
				uniqueIDs[scooter.Identifier] = true
			}
		}
	}

	scooterIDs := make([]string, 0, len(uniqueIDs))
	for id, _ := range uniqueIDs {
		scooterIDs = append(scooterIDs, id)
	}
	return scooterIDs, nil
}

type scooters map[string]*circ.Scooter

func newScooters(in []*circ.Scooter) scooters {
	s := make(scooters)
	for _, scooter := range in {
		s[scooter.Identifier] = scooter
	}
	return s
}

func (s scooters) difference(ns scooters) scooters {
	s2 := make(scooters)

	for id, scooter := range ns {
		if _, exists := s[id]; !exists {
			s2[id] = scooter
		}
	}
	return s2
}
