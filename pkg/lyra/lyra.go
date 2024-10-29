package lyra

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	appsv1alpha1 "github.com/azhry/lyrid-operator/api/v1alpha1"
	"github.com/azhry/lyrid-operator/pkg/dto"
	corev1 "k8s.io/api/core/v1"
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
	requestBody := struct {
		AppName      string                      `json:"app_name"`
		AppNamespace string                      `json:"app_namespace"`
		Replicas     int32                       `json:"replicas"`
		Ports        []corev1.ContainerPort      `json:"ports"`
		Resources    corev1.ResourceRequirements `json:"resources"`
		VolumeMounts []corev1.VolumeMount        `json:"volume_mounts"`
	}{
		AppName:      appDeployment.Name,
		AppNamespace: appDeployment.Namespace,
		Replicas:     appDeployment.Spec.Replicas,
		Ports:        appDeployment.Spec.Ports,
		Resources:    appDeployment.Spec.Resources,
		VolumeMounts: appDeployment.Spec.VolumeMounts,
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
