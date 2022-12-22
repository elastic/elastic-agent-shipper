package environments

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/elastic/elastic-agent-shipper/tools"
	"github.com/magefile/mage/mg"
)

type testImage struct {
	name string
	path string
}

type testImageConfig struct {
	image       testImage         // The docker image
	environment map[string]string // Environment variables for docker-compose
}

// The raw Docker images used by test configurations. New Docker images
// should be added to this list.
var (
	elasticsearchImage = testImage{
		name: "Elasticsearch",
		path: "testing/environments/elasticsearch/docker-compose.yml",
	}

	kafkaImage = testImage{
		name: "Kafka",
		path: "testing/environments/kafka/docker-compose.yml",
	}
)

func DefaultElasticsearch() TestImageConfig {
	basePath := "docker.elastic.co/elasticsearch/elasticsearch"
	version := tools.DefaultBeatVersion + "-SNAPSHOT"
	return testImageConfig{
		image: elasticsearchImage,
		environment: map[string]string{
			"ELASTICSEARCH_IMAGE_REF": fmt.Sprintf("%v:%v", basePath, version),
		},
	}
}

func DefaultKafka() TestImageConfig {
	return testImageConfig{image: kafkaImage}
}

func Up(configs []TestImageConfig) error {
	cmd := dockerComposeCommand([]string{"up", "-d"}, configs)

	return cmd.Run()
}

func Down(configs []TestImageConfig) error {
	cmd := dockerComposeCommand([]string{"down"}, configs)

	return cmd.Run()
}

func dockerComposeCommand(args []string, configs []TestImageConfig) *exec.Cmd {
	env := os.Environ()
	for _, config := range configs {
		args = append(args, "-f", config.im().path)
		for key, value := range config.env() {
			env = append(env, key+"="+value)
		}
	}
	cmd := exec.Command("docker-compose", args...)
	cmd.Env = append(os.Environ(), env...)

	if mg.Verbose() {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd
}

// This interface wrapper is to make the internal representation of the
// test images and configuration opaque to callers, since we will probably
// want to change them as we expand to cover more environments.
type TestImageConfig interface {
	im() testImage          // The docker image
	env() map[string]string // Environment variables for docker-compose
}

func (tic testImageConfig) im() testImage {
	return tic.image
}

func (tic testImageConfig) env() map[string]string {
	return tic.environment
}
