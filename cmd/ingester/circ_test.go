package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDateParsing(t *testing.T) {
	fileName := "circ_2019-10-08T05:11:27+01:00.json.gz"

	date, err := extractDateFromFilename(fileName)
	require.NoError(t, err)
	assert.NotEmpty(t, date)
}
