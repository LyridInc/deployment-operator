package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/azhry/lyrid-operator/pkg/dto"
)

func DoLyraHttpRequest(method, path, token string, args []byte) ([]byte, error) {
	var body *bytes.Buffer
	if args != nil {
		body = bytes.NewBuffer(args)
	}

	lyraUrl := os.Getenv("LYRA_URL")
	request, err := http.NewRequest(method, lyraUrl+path, body)
	if err != nil {
		return nil, err
	}

	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{}
	response, err := client.Do(request)

	if err != nil {
		return nil, err
	}

	if response.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("error %d: resource not found for %s", response.StatusCode, request.URL)
	}

	defer response.Body.Close()

	b, err := io.ReadAll(response.Body)
	return b, err
}

func Authenticate(accessKey, accessSecret string) (*dto.AuthResponseDTO, error) {
	requestBody := struct {
		KeyRequest    string `json:"key"`
		SecretRequest string `json:"secret"`
	}{
		KeyRequest:    accessKey,
		SecretRequest: accessSecret,
	}

	// Encode the request body into JSON
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		fmt.Println("Error encoding JSON:", err)
		return nil, err
	}

	resp, err := DoLyraHttpRequest("POST", "/auth", "", jsonData)
	if err != nil {
		fmt.Println("Error http request:", err)
		return nil, err
	}

	var authResponse dto.AuthResponseDTO
	if err := json.Unmarshal(resp, &authResponse); err != nil {
		fmt.Println("Error unmarshal response:", err)
		return nil, err
	}

	return &authResponse, err
}

func GetCachedTokenByNamespace(namespace string) {

}
