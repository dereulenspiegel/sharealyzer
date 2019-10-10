package circ

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// FileTokenStore is a simple TokenStore which saves the auth tokens in a simple, unencrypted(!) file
type FileTokenStore struct {
	Path string
}

type tokenData struct {
	AccessToken  string
	RefreshToken string
}

// Store stores the tokens in the file. The file is deleted and recreated for this
func (f *FileTokenStore) Store(accessToken, refreshToken string) (err error) {
	dir := filepath.Dir(f.Path)
	var tokenFile *os.File
	if fileDoesNotExist(dir) {
		if err := os.MkdirAll(dir, 0660); err != nil {
			return err
		}
	}
	os.Remove(f.Path)
	tokenFile, err = os.Create(f.Path)
	if err != nil {
		return err
	}
	defer tokenFile.Close()

	if err := json.NewEncoder(tokenFile).Encode(&tokenData{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}); err != nil {
		return err
	}
	return nil
}

// Load loads the tokens from the file if the file exists and contains tokens.
func (f *FileTokenStore) Load() (accessToken string, refreshToken string, err error) {
	var tokenFile *os.File
	tokenFile, err = os.Open(f.Path)
	if err != nil {
		return
	}
	defer tokenFile.Close()
	var data tokenData
	if err = json.NewDecoder(tokenFile).Decode(&data); err != nil {
		return
	}
	return data.AccessToken, data.RefreshToken, nil
}

func fileDoesNotExist(path string) bool {
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
