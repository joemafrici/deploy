package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
)

func main() {
	apiClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}
	defer apiClient.Close()

	tarBytes, err := archive.Tar("../../imgserv", archive.Uncompressed)
	if err != nil {
		panic(err)
	}

	var pathToDockerfile = "Dockerfile"
	buildOptions := types.ImageBuildOptions{
		Dockerfile: pathToDockerfile,
	}
	resp, err := apiClient.ImageBuild(context.TODO(), tarBytes, buildOptions)
	if err != nil {
		panic(err)
	}

	decoder := json.NewDecoder(resp.Body)
	for {
		var buildResp struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}

		if err := decoder.Decode(&buildResp); err != nil {
			if err == io.EOF {
				break
			}
			panic(err)
		}

		if buildResp.Error != "" {
			panic(buildResp.Error)
		}

		if buildResp.Stream != "" {
			fmt.Print(buildResp.Stream) // stream already includes newlines
		}
	}
}
