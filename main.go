package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/go-connections/nat"
	"golang.org/x/crypto/ssh"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type proxyConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (c *proxyConn) Read(b []byte) (int, error) {
	return c.stdout.Read(b)
}
func (c *proxyConn) Write(b []byte) (int, error) {
	return c.stdin.Write(b)
}
func (c *proxyConn) Close() error {
	c.stdin.Close()
	c.stdout.Close()
	if err := c.cmd.Process.Kill(); err != nil {
		return err
	}
	return c.cmd.Wait()
}

// Required net.Conn interface methods
func (c *proxyConn) LocalAddr() net.Addr                { return nil }
func (c *proxyConn) RemoteAddr() net.Addr               { return nil }
func (c *proxyConn) SetDeadline(t time.Time) error      { return nil }
func (c *proxyConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *proxyConn) SetWriteDeadline(t time.Time) error { return nil }

func main() {
	keyPath := os.ExpandEnv("$HOME/.ssh/id_ed25519")
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		panic(err)
	}

	key, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		panic(err)
	}

	username := "deepwater"
	sshClientConfig := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(key),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}
	host := "ssh.gojoe.dev"

	cmd := exec.Command("cloudflared", "access", "ssh", "--hostname", host)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		panic(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		panic(err)
	}

	if err := cmd.Start(); err != nil {
		panic(err)
	}

	conn := &proxyConn{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, host, sshClientConfig)
	if err != nil {
		panic(err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)
	fmt.Printf("connected to %s\n", host)
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		panic(err)
	}
	defer session.Close()

	output, err := session.Output("docker ps") // This will run 'docker ps' on the remote machine
	if err != nil {
		panic(err)
	}
	fmt.Println(string(output))

	dockerClient, err := dockerclient.NewClientWithOpts(
		dockerclient.WithDialContext(func(ctx context.Context, network, addr string) (net.Conn, error) {
			return client.Dial("unix", "/var/run/docker.sock")
		}),
		dockerclient.WithHost("unix:///var/run/docker.sock"),
		dockerclient.WithAPIVersionNegotiation(),
	)
	if err != nil {
		panic(err)
	}
	defer dockerClient.Close()

	containers, err := dockerClient.ContainerList(context.TODO(), container.ListOptions{})
	if err != nil {
		panic(err)
	}

	for idx, ctr := range containers {
		fmt.Printf("Container %v: %s %s\n", idx, ctr.ID, ctr.Image)
	}

	// TODO: some of this stuff is going to be done with a local docker client
	// TODO: and some of it will be done with the remote docker client
	// TODO: EX: building image done on local client????
	//resp, err := buildImage(dockerClient, "../../imgserv")
	//if err != nil {
	//	panic(err)
	//}
	//imageID, err := processResponseStream(resp.Body)
	//if err != nil {
	//	panic(err)
	//}

	//containerID, err := buildContainer(dockerClient, imageID, "test-container-name")
	//if err != nil {
	//	panic(err)
	//}
	//fmt.Printf("Container ID is %s\n", containerID)

	//err = startContainer(dockerClient, containerID)
	//if err != nil {
	//	panic(err)
	//}
	//fmt.Printf("Container %s started\n", containerID)

}

func startContainer(client *dockerclient.Client, containerID string) error {
	err := client.ContainerStart(context.TODO(), containerID, container.StartOptions{})
	if err != nil {
		return err
	}
	return nil
}

func buildContainer(client *dockerclient.Client, imageID string, containerName string) (string, error) {
	fmt.Println("building container...")
	fmt.Printf("imageID is %s\ncontainer name is %s\n", imageID, containerName)
	containerPort, err := nat.NewPort("tcp", "80")
	if err != nil {
		return "", err
	}

	config := &container.Config{
		Image: imageID,
		ExposedPorts: nat.PortSet{
			containerPort: struct{}{},
		},
	}

	absPath, err := filepath.Abs("../../imgserv/assets")
	if err != nil {
		return "", err
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			containerPort: []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: "3003",
				},
			},
		},
		Binds: []string{
			fmt.Sprintf("%s:/app/assets", absPath),
		},
	}
	resp, err := client.ContainerCreate(context.TODO(), config, hostConfig, &network.NetworkingConfig{}, &ocispec.Platform{}, containerName)
	if err != nil {
		return "", err
	}
	if len(resp.Warnings) > 0 {
		fmt.Printf("there were %v warnings generated", len(resp.Warnings))
		for idx, warning := range resp.Warnings {
			fmt.Printf("warning %v -> %v", idx, warning)
		}

	}
	return resp.ID, err
}

func buildImage(client *dockerclient.Client, path string) (types.ImageBuildResponse, error) {
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

// Prints response stream and returns image id
func processResponseStream(stream io.Reader) (string, error) {
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
			return "", err
		}

		if buildResp.Error != "" {
			return "", errors.New(buildResp.Error)
		}

		if buildResp.Stream != "" {
			fmt.Print(buildResp.Stream) // stream already includes newlines

			if strings.HasPrefix(buildResp.Stream, "Successfully built ") {
				return strings.TrimSpace(strings.TrimPrefix(buildResp.Stream, "Successfully built ")), nil
			}
		}
	}

	return "", nil
}
