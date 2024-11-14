package main

import (
	"context"
	"encoding/json"
	"errors"
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

	resp, err := buildImage(apiClient, "../../imgserv")
	if err != nil {
		panic(err)
	}
	err = printRespStream(resp.Body)
	if err != nil {
		panic(err)
	}
}

func buildImage(client *client.Client, path string) (types.ImageBuildResponse, error) {
	tarBytes, err := archive.Tar(path, archive.Uncompressed)
	if err != nil {
		return types.ImageBuildResponse{}, err
	}

	var pathToDockerfile = "Dockerfile"
	buildOptions := types.ImageBuildOptions{
		Dockerfile: pathToDockerfile,
	}
	resp, err := client.ImageBuild(context.TODO(), tarBytes, buildOptions)
	if err != nil {
		return types.ImageBuildResponse{}, err
	}
	return resp, nil
}

func printRespStream(stream io.Reader) error {
	decoder := json.NewDecoder(stream)
	for {
		var buildResp struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}

		if err := decoder.Decode(&buildResp); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		if buildResp.Error != "" {
			return errors.New(buildResp.Error)
		}

		if buildResp.Stream != "" {
			fmt.Print(buildResp.Stream) // stream already includes newlines
		}
	}

	return nil
}
