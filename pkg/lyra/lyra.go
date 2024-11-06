package lyra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	lyrmodel "github.com/LyridInc/go-sdk/model"
	appsv1alpha1 "github.com/azhry/lyrid-operator/api/v1alpha1"
	"github.com/azhry/lyrid-operator/pkg/dto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type LyraClient struct {
	BaseURL    string
	Client     *http.Client
	Tokens     map[string]string
	KubeClient client.Client
}

func NewLyraClient(kubeClient client.Client, namespace string) *LyraClient {
	vegaConfig := &corev1.ConfigMap{}
	if err := kubeClient.Get(context.Background(), types.NamespacedName{Name: "vega-init", Namespace: namespace}, vegaConfig); err != nil {
		if errors.IsNotFound(err) {
			fmt.Printf("vega-init ConfigMap is not found in namespace %s\n", namespace)
		}
		return nil
	}
	return &LyraClient{
		Client:     &http.Client{},
		BaseURL:    os.Getenv("LYRA_URL"),
		Tokens:     map[string]string{},
		KubeClient: kubeClient,
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

func (c *LyraClient) SyncApp(appDeployment appsv1alpha1.AppDeployment, accessKey, accessSecret string) (*lyrmodel.SyncAppResponse, error) {
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

	fmt.Println(string(jsonData))

	token := c.GetCachedTokenByNamespace(requestBody.AppNamespace)
	if token == nil {
		respToken, err := c.Authenticate(accessKey, accessSecret)
		if err != nil {
			return nil, err
		}
		token = &respToken.Token
	}

	resp, err := c.DoLyraHttpRequest("POST", "/operator/app/sync", *token, jsonData)
	if err != nil {
		fmt.Println("Error http request:", err)
		return nil, err
	}

	syncAppResponse := lyrmodel.SyncAppResponse{}
	if err := json.Unmarshal(resp, &syncAppResponse); err != nil {
		return nil, err
	}

	return &syncAppResponse, nil
}
