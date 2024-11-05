package lyra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	lyrmodel "github.com/LyridInc/go-sdk/model"
	appsv1alpha1 "github.com/azhry/lyrid-operator/api/v1alpha1"
	"github.com/azhry/lyrid-operator/pkg/dto"
)

type LyraClient struct {
	BaseURL string
	Client  *http.Client
	Tokens  map[string]string
}

func NewLyraClient() *LyraClient {
	return &LyraClient{
		Client:  &http.Client{},
		BaseURL: os.Getenv("LYRA_URL"),
		Tokens:  map[string]string{},
	}
}

func (c *LyraClient) DoLyraHttpRequest(method, path, token string, args []byte) ([]byte, error) {
	var body *bytes.Buffer
	if args != nil {
		body = bytes.NewBuffer(args)
	}

	request, err := http.NewRequest(method, c.BaseURL+path, body)
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

func (c *LyraClient) Authenticate(accessKey, accessSecret string) (*dto.AuthResponseDTO, error) {
	requestBody := struct {
		KeyRequest    string `json:"key"`
		SecretRequest string `json:"secret"`
	}{
		KeyRequest:    accessKey,
		SecretRequest: accessSecret,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		fmt.Println("Error encoding JSON:", err)
		return nil, err
	}

	resp, err := c.DoLyraHttpRequest("POST", "/auth", "", jsonData)
	if err != nil {
		fmt.Println("Error http request:", err)
		return nil, err
	}

	var authResponse dto.AuthResponseDTO
	if err := json.Unmarshal(resp, &authResponse); err != nil {
		fmt.Println("Error unmarshal response:", err)
		return nil, err
	}

	c.Tokens[authResponse.AccountID] = authResponse.Token

	return &authResponse, nil
}

func (c *LyraClient) GetCachedTokenByNamespace(namespace string) *string {
	token, ok := c.Tokens[namespace]
	if !ok {
		return nil
	}
	return &token
}

func (c *LyraClient) SyncApp(appDeployment appsv1alpha1.AppDeployment, accessKey, accessSecret string) (interface{}, error) {
	resources := lyrmodel.SyncAppResources{
		Limits: lyrmodel.SyncAppResource{
			Cpu:    appDeployment.Spec.Resources.Limits.Cpu().String(),
			Memory: appDeployment.Spec.Resources.Limits.Memory().String(),
		},
		Requests: lyrmodel.SyncAppResource{
			Cpu:    appDeployment.Spec.Resources.Requests.Cpu().String(),
			Memory: appDeployment.Spec.Resources.Requests.Memory().String(),
		},
	}

	ports := []lyrmodel.ContainerPort{}
	for _, p := range appDeployment.Spec.Ports {
		ports = append(ports, lyrmodel.ContainerPort{
			Name:          p.Name,
			ContainerPort: p.ContainerPort,
		})
	}

	volumeMount := lyrmodel.VolumeMount{}
	if len(appDeployment.Spec.VolumeMounts) > 0 {
		vmnt := appDeployment.Spec.VolumeMounts[0]
		volumeMount.Name = vmnt.Name
		volumeMount.MountPath = vmnt.MountPath
	}

	requestBody := lyrmodel.SyncAppRequest{
		AppName:      appDeployment.Name,
		AppNamespace: appDeployment.Namespace,
		Replicas:     appDeployment.Spec.Replicas,
		Ports:        ports,
		Resources:    resources,
		VolumeMounts: volumeMount,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		fmt.Println("Error encoding JSON:", err)
		return nil, err
	}

	token := c.GetCachedTokenByNamespace(requestBody.AppNamespace)
	if token == nil {
		respToken, err := c.Authenticate(accessKey, accessSecret)
		if err != nil {
			return nil, err
		}
		token = &respToken.Token
	}

	_, err = c.DoLyraHttpRequest("POST", "/operator/app/sync", *token, jsonData)
	if err != nil {
		fmt.Println("Error http request:", err)
		return nil, err
	}
	return nil, nil
}
