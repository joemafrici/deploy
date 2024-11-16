package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	//"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
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
	dockerhubToken, sshClient, dockerClientLocal, dockerClientRemote, err := setup()
	if err != nil {
		panic(err)
	}
	defer sshClient.Close()
	defer dockerClientLocal.Close()
	defer dockerClientRemote.Close()

	//sshSession, err := sshClient.NewSession()
	//if err != nil {
	//	panic(err)
	//}
	//defer sshSession.Close()

	//output, err := sshSession.Output("cd imgserv && git pull")
	//if err != nil {
	//	panic(err)
	//}
	//fmt.Println(string(output))

	fmt.Println("Building image")
	resp, err := buildImage(dockerClientLocal, "../hello_world")
	if err != nil {
		panic(err)
	}
	imageID, err := processResponseStream(resp.Body)
	if err != nil {
		panic(err)
	}

	fmt.Println("Tagging image as gojoe2/hello_world:latest")
	err = dockerClientLocal.ImageTag(context.TODO(), imageID, "gojoe2/hello_world:latest")
	if err != nil {
		panic(err)
	}

	authConfig := registry.AuthConfig{
		Username: "gojoe2",
		Password: dockerhubToken,
	}
	encodedJSON, err := json.Marshal(authConfig)
	if err != nil {
		panic(err)
	}
	authStr := base64.URLEncoding.EncodeToString(encodedJSON)

	fmt.Println("Pushing image")
	respImagePush, err := dockerClientLocal.ImagePush(context.TODO(), "gojoe2/hello_world:latest",
		image.PushOptions{
			RegistryAuth: authStr,
		})
	if err != nil {
		panic(err)
	}
	defer respImagePush.Close()

	if err := printResponseStream(respImagePush); err != nil {
		panic(err)
	}

	fmt.Println("Pulling image on remote")
	respImagePull, err := dockerClientRemote.ImagePull(context.TODO(), "gojoe2/hello_world:latest",
		image.PullOptions{
			RegistryAuth: authStr,
		})
	if err != nil {
		panic(err)
	}
	defer respImagePull.Close()

	if err := printResponseStream(respImagePull); err != nil {
		panic(err)
	}

	fmt.Println("Attempting to stop container hello_world")
	err = dockerClientRemote.ContainerStop(context.TODO(), "hello_world", container.StopOptions{})
	if err != nil {
		fmt.Printf("%v\n", err)
	}

	fmt.Println("Attempting to remove container hello_world")
	err = dockerClientRemote.ContainerRemove(context.TODO(), "hello_world", container.RemoveOptions{})
	if err != nil {
		fmt.Printf("%v\n", err)
	}

	fmt.Println("Building container hello_world")
	containerID, err := buildContainer(dockerClientRemote, imageID, "hello_world")
	if err != nil {
		panic(err)
	}
	fmt.Printf("Container ID is %s\n", containerID)

	err = startContainer(dockerClientRemote, containerID)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Container %s started\n", containerID)
}

func printResponseStream(stream io.ReadCloser) error {
	decoder := json.NewDecoder(stream)
	for {
		var pushResponse struct {
			Status   string `json:"status"`
			Error    string `json:"error"`
			Progress string `json:"progress,omitempty"`
		}

		if err := decoder.Decode(&pushResponse); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		if pushResponse.Error != "" {
			return errors.New(pushResponse.Error)
		}

		fmt.Printf("%s\n", pushResponse.Status)
		if pushResponse.Progress != "" {
			fmt.Printf("%s\n", pushResponse.Progress)
		}
	}
	return nil
}

// caller is in charge of closing things
func setup() (string, *ssh.Client, *dockerclient.Client, *dockerclient.Client, error) {
	dockerhubToken, err := getDockerhubToken()
	if err != nil {
		return "", nil, nil, nil, err
	}

	sshClient, err := newSSHClient()
	if err != nil {
		return "", nil, nil, nil, err
	}

	dockerClientLocal, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv)
	if err != nil {
		return "", nil, nil, nil, err
	}

	dockerClientRemote, err := newDockerClientRemote(sshClient)
	if err != nil {
		return "", nil, nil, nil, err
	}

	return dockerhubToken, sshClient, dockerClientLocal, dockerClientRemote, nil
}

func getDockerhubToken() (string, error) {
	fileBytes, err := os.ReadFile(".dockerhub")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(fileBytes)), nil
}

func listAllContainers(client *dockerclient.Client) {
	containers, err := client.ContainerList(context.TODO(), container.ListOptions{})
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}

	for idx, ctr := range containers {
		fmt.Printf("Container %v: %s %s\n", idx, ctr.ID, ctr.Image)
	}
}

func newDockerClientRemote(sshClient *ssh.Client) (*dockerclient.Client, error) {
	return dockerclient.NewClientWithOpts(
		dockerclient.WithDialContext(func(ctx context.Context, network, addr string) (net.Conn, error) {
			return sshClient.Dial("unix", "/var/run/docker.sock")
		}),
		dockerclient.WithAPIVersionNegotiation(),
	)
}
func newSSHClient() (*ssh.Client, error) {
	keyPath := os.ExpandEnv("$HOME/.ssh/id_ed25519")
	keyBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}

	key, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	conn := &proxyConn{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, host, sshClientConfig)
	if err != nil {
		return nil, err
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

func startContainer(client *dockerclient.Client, containerID string) error {
	err := client.ContainerStart(context.TODO(), containerID, container.StartOptions{})
	if err != nil {
		return err
	}
	return nil
}

func buildContainer(client *dockerclient.Client, imageID string, containerName string) (string, error) {
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

	//absPath, err := filepath.Abs("../../imgserv/assets")
	//if err != nil {
	//	return "", err
	//}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			containerPort: []nat.PortBinding{
				{
					HostIP:   "0.0.0.0",
					HostPort: "3004",
				},
			},
		},
		//Binds: []string{
		//	fmt.Sprintf("%s:/app/assets", absPath),
		//},
	}
	networkConfig := network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			"deepwater_web": {},
		},
	}
	resp, err := client.ContainerCreate(context.TODO(), config, hostConfig, &networkConfig, &ocispec.Platform{}, containerName)
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
