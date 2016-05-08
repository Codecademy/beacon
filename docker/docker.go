package docker

import (
	"crypto/tls"
	"github.com/BlueDragonX/beacon/container"
	"github.com/BlueDragonX/dockerclient"
	"strconv"
	"time"
)

// Docker provides container events from a Docker container runtime.
type Docker struct {
	client     *dockerclient.DockerClient
	interval   time.Duration
	containers map[string]*container.Container
	stopped    chan struct{}
}

// NewDocker creates a Docker object connected to `uri`. It will listen for
// events and poll after `interval` to ensure no events were missed. TLS may be
// enabled by providing a non-nil value to `tls`.
func NewDocker(uri string, interval time.Duration, tls *tls.Config) (*Docker, error) {
	if client, err := dockerclient.NewDockerClient(uri, tls); err == nil {
		docker := &Docker{
			client,
			interval,
			make(map[string]*container.Container),
			make(chan struct{}),
		}
		return docker, nil
	} else {
		return nil, err
	}
}

// Listen for container events and queue them into `events`.
func (docker *Docker) Listen(events chan<- *container.Event) {
	logger.Printf("docker listener started")

	// listen for events from docker
	clientEvents := make(chan *dockerclient.Event)
	clientErrors := make(chan error)

	startMonitor := func() {
		docker.client.StartMonitorEvents(func(e *dockerclient.Event, _ chan error, _ ...interface{}) {
			clientEvents <- e
		}, clientErrors)
	}
	go startMonitor()

	// do an initial poll to load the current containers
	docker.poll(events)

	// process client events and poll periodically
	ticker := time.NewTicker(docker.interval)
	defer ticker.Stop()
Loop:
	for {
		select {
		case e := <-clientEvents:
			// process client events from monitor
			if e.Status == "start" || e.Status == "unpause" {
				docker.add(e.Id, events)
				logger.Printf("event %s added container %s", e.Status, e.Id)
			} else if e.Status == "die" || e.Status == "kill" || e.Status == "pause" {
				docker.remove(e.Id, events)
				logger.Printf("event %s removed container %s", e.Status, e.Id)
			} else {
				logger.Printf("event %s ignored for container %s", e.Status, e.Id)
			}
		case err := <-clientErrors:
			// monitor failed, restart it
			logger.Printf("client monitor failed: %s", err)
			go startMonitor()
		case <-ticker.C:
			// poll for container list
			docker.poll(events)
		case <-docker.stopped:
			docker.client.StopAllMonitorEvents()
			break Loop
		}
	}
	logger.Printf("docker listener stopped")
}

// Close stops listening for container events.
func (docker *Docker) Close() error {
	close(docker.stopped)
	return nil
}

func (docker *Docker) poll(events chan<- *container.Event) {
	logger.Printf("docker poll started")
	containers, err := docker.client.ListContainers(false, false, "")
	if err != nil {
		logger.Printf("list containers failed: %s", err)
	}
	ids := make(map[string]struct{}, len(containers))
	for _, cntr := range containers {
		ids[cntr.Id] = struct{}{}
		docker.add(cntr.Id, events)
	}
	for id := range docker.containers {
		if _, has := ids[id]; !has {
			docker.remove(id, events)
		}
	}
	logger.Printf("docker poll complete")
}

// add emits an Add event for the container with the given id.
func (docker *Docker) add(id string, events chan<- *container.Event) {
	if _, has := docker.containers[id]; has {
		return
	}
	if cntr := docker.get(id); cntr != nil {
		logger.Printf("docker started container %s", id)
		docker.containers[id] = cntr
		events <- &container.Event{
			Action:    container.Add,
			Container: cntr,
		}
	}
}

// remove emits a Remove event for the container with the given id.
func (docker *Docker) remove(id string, events chan<- *container.Event) {
	if cntr, has := docker.containers[id]; has {
		logger.Printf("docker stopped container %s", id)
		delete(docker.containers, id)
		events <- &container.Event{
			Action:    container.Remove,
			Container: cntr,
		}
	}
}

// get the container which has the given id. Logs an error and returns nil if not found.
func (docker *Docker) get(id string) *container.Container {
	errorFmt := "docker inspect failed on %s: %s"
	info, err := docker.client.InspectContainer(id)
	if err != nil {
		logger.Printf(errorFmt, id, err)
		return nil
	}

	mappings := []*container.Mapping{}
	for port, bindings := range info.NetworkSettings.Ports {
		containerPort, err := container.ParsePort(port)
		if err != nil {
			logger.Printf(errorFmt, id, err)
			return nil
		}
		for _, binding := range bindings {
			hostPort, err := strconv.Atoi(binding.HostPort)
			if err != nil {
				logger.Printf(errorFmt, id, err)
				return nil
			}
			hostAddress := &container.Address{
				Hostname: binding.HostIp,
				Port: &container.Port{
					Number:   hostPort,
					Protocol: containerPort.Protocol,
				},
			}
			mappings = append(mappings, &container.Mapping{
				HostAddress:   hostAddress,
				ContainerPort: containerPort,
			})
		}
	}

	return &container.Container{
		ID:       info.Id,
		Environ:  info.Config.Env,
		Hostname: info.NetworkSettings.IPAddress,
		Mappings: mappings,
	}
}