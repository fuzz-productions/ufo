package ufo

import (
	"fmt"
	"regexp"
	"runtime"
	"strings"

	"github.com/aws/aws-sdk-go/aws/awserr"

	"github.com/aws/aws-sdk-go/service/ecr/ecriface"
	"github.com/aws/aws-sdk-go/service/ecs/ecsiface"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecr"
	"github.com/aws/aws-sdk-go/service/ecs"
)

type Logger interface {
	Printf(string, ...interface{})
}

type logger struct{}

func (l *logger) Printf(format string, a ...interface{}) {
	fmt.Printf(format, a...)
}

type Config struct {
	Profile *string
	Region  *string
}

type State struct {
	Cluster        *ecs.Cluster
	Service        *ecs.Service
	TaskDefinition *ecs.TaskDefinition
}

type UFO struct {
	l     Logger
	State *State
	ECS   ecsiface.ECSAPI
	ECR   ecriface.ECRAPI
}

// Fly is an alias for CreateUFO
func Fly(c Config) *UFO {
	return New(c)
}

// New creates a UFO session and connects to AWS to create a session
func New(c Config) *UFO {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		Config:  aws.Config{Region: c.Region},
		Profile: *c.Profile,
	}))

	app := &UFO{
		l:     &logger{},
		ECS:   ecs.New(sess),
		ECR:   ecr.New(sess),
		State: &State{},
	}

	return app
}

// UseCluster sets a cluster choice in UFO state
// @todo this may be extraneous but if we decide to leave it in, we should have funcs optionally require
//		the cluster/service/taskDef and if not passed, can use the ones stored in state.
func (u *UFO) UseCluster(c *ecs.Cluster) {
	u.State.Cluster = c
}

// UseService sets a service choice in UFO state
func (u *UFO) UseService(s *ecs.Service) {
	u.State.Service = s
}

// UseTaskDefinition sets a task definition choice in UFO state
func (u *UFO) UseTaskDefinition(t *ecs.TaskDefinition) {
	u.State.TaskDefinition = t
}

// Handle errors, nil or otherwise
// This func is intended to always be called after an error is returned from an AWS method call in UFO
func (u *UFO) logError(err error) {
	switch err := err.(type) {
	case awserr.Error:
		if parsed, ok := err.(awserr.Error); ok {
			pc := make([]uintptr, 15)
			n := runtime.Callers(2, pc)
			frames := runtime.CallersFrames(pc[:n])
			frame, _ := frames.Next()

			u.l.Printf("Code: %s. %s\n %s,:%d %s\n", parsed.Code(), parsed.Error(), frame.File, frame.Line, frame.Function)
		}
	default:
		u.l.Printf("error: %v", err)
	}
}

// Clusters returns all ECS clusters in the account
func (u *UFO) Clusters() ([]string, error) {
	res, err := u.ECS.ListClusters(&ecs.ListClustersInput{})

	if err != nil {
		u.logError(err)

		return nil, ErrFailedToListClusters
	}

	r := regexp.MustCompile(`([^\/]+)$`)
	clusters := make([]string, len(res.ClusterArns))

	for i, cluster := range res.ClusterArns {
		clusters[i] = r.FindString(*cluster)
	}

	return clusters, nil
}

// Services returns all services in a cluster
func (u *UFO) Services(c *ecs.Cluster) ([]string, error) {
	res, err := u.ECS.ListServices(&ecs.ListServicesInput{
		Cluster: c.ClusterArn,
	})

	if err != nil {
		u.logError(err)

		return nil, ErrFailedToListServices
	}

	r := regexp.MustCompile(`([^\/]+)$`)
	services := make([]string, len(res.ServiceArns))

	for i, service := range res.ServiceArns {
		services[i] = r.FindString(*service)
	}

	return services, nil
}

// RunningTasks gets all running tasks in a cluster and service
func (u *UFO) RunningTasks(c *ecs.Cluster, s *ecs.Service) ([]*string, error) {
	result, err := u.ECS.ListTasks(&ecs.ListTasksInput{
		Cluster:       c.ClusterName,
		ServiceName:   s.ServiceName,
		DesiredStatus: aws.String("RUNNING"),
	})

	if err != nil {
		u.logError(err)

		return nil, ErrFailedToListRunningTasks
	}

	return result.TaskArns, nil
}

// GetCluster returns a clusters detail with cluster name or ARN
func (u *UFO) GetCluster(name string) (*ecs.Cluster, error) {
	res, err := u.ECS.DescribeClusters(&ecs.DescribeClustersInput{
		Clusters: []*string{
			&name,
		},
	})

	if err != nil {
		u.logError(err)

		return nil, ErrCouldNotRetrieveCluster
	}

	if len(res.Clusters) < 1 {
		return nil, ErrClusterNotFound
	}

	return res.Clusters[0], nil
}

// GetService returns service details within a cluster by service name or ARN
func (u *UFO) GetService(c *ecs.Cluster, service string) (*ecs.Service, error) {
	res, err := u.ECS.DescribeServices(&ecs.DescribeServicesInput{
		Cluster: c.ClusterArn,
		Services: []*string{
			&service,
		},
	})

	if err != nil {
		u.logError(err)

		return nil, ErrCouldNotRetrieveService
	}

	if len(res.Services) < 1 {
		return nil, ErrServiceNotFound
	}

	return res.Services[0], nil
}

// GetTaskDefinition returns details of a task definition in
// a cluster and service by service's current task definition
func (u *UFO) GetTaskDefinition(c *ecs.Cluster, s *ecs.Service) (*ecs.TaskDefinition, error) {
	result, err := u.ECS.DescribeTaskDefinition(&ecs.DescribeTaskDefinitionInput{
		TaskDefinition: s.TaskDefinition,
	})

	if err != nil {
		u.logError(err)

		return nil, ErrCouldNotRetrieveTaskDefinition
	}

	return result.TaskDefinition, nil
}

// GetTasks gets all tasks in a cluster
func (u *UFO) GetTasks(c *ecs.Cluster, tasks []*string) ([]*ecs.Task, error) {
	result, err := u.ECS.DescribeTasks(&ecs.DescribeTasksInput{
		Cluster: c.ClusterName,
		Tasks:   tasks,
	})

	if err != nil {
		u.logError(err)

		return nil, ErrCouldNotRetrieveTasks
	}

	return result.Tasks, nil
}

// GetImages gets images for a task definition
// @todo how does this handle multiple images in a task def?
func (u *UFO) GetImages(t *ecs.TaskDefinition) ([]*ecr.ImageDetail, error) {
	currentImage := t.ContainerDefinitions[0].Image

	// Parse the repo name out of an image tag
	repoName := u.GetRepoFromImage(currentImage)

	result, err := u.ECR.DescribeImages(&ecr.DescribeImagesInput{
		RepositoryName: aws.String(repoName),
	})

	if err != nil {
		u.logError(err)

		return nil, ErrCouldNotRetrieveImages
	}

	images := make([]*ecr.ImageDetail, 0)

	for _, image := range result.ImageDetails {
		if image.ImageTags != nil {
			images = append(images, image)
		}
	}

	return images, nil
}

// GetLastDeployedCommit finds the most recent committed image for a taskDefinition
func (u *UFO) GetLastDeployedCommit(taskDefinition string) (string, error) {
	result, err := u.ECS.DescribeTaskDefinition(&ecs.DescribeTaskDefinitionInput{
		TaskDefinition: &taskDefinition,
	})

	if err != nil {
		u.logError(err)

		return "", ErrCouldNotRetrieveTaskDefinition
	}

	if len(result.TaskDefinition.ContainerDefinitions) < 1 {
		return "", ErrInvalidTaskDefinition
	}

	repo := result.TaskDefinition.ContainerDefinitions[0].Image

	r := regexp.MustCompile(`\:(\S+)`)

	return r.FindStringSubmatch(*repo)[1], nil
}

// RegisterTaskDefinitionWithImage creates a new task definition with an image at a specific version
// This copies an existing task definition and only updates the tag used for the image
func (u *UFO) RegisterTaskDefinitionWithImage(c *ecs.Cluster, s *ecs.Service, version string) (*ecs.TaskDefinition, error) {
	t, err := u.GetTaskDefinition(c, s)

	if err != nil {
		u.logError(err)

		return nil, err // @todo simplify return
	}

	newTaskDef := u.UpdateTaskDefinitionImage(*t, version)

	result, err := u.ECS.RegisterTaskDefinition(&ecs.RegisterTaskDefinitionInput{
		// Update the task definition to use the new docker image via UpdateTaskDefinitionImage
		Cpu:                     newTaskDef.Cpu,
		Family:                  newTaskDef.Family,
		Memory:                  newTaskDef.Memory,
		Volumes:                 newTaskDef.Volumes,
		NetworkMode:             newTaskDef.NetworkMode,
		ExecutionRoleArn:        newTaskDef.ExecutionRoleArn,
		TaskRoleArn:             newTaskDef.TaskRoleArn,
		ContainerDefinitions:    newTaskDef.ContainerDefinitions,
		RequiresCompatibilities: newTaskDef.RequiresCompatibilities,
	})

	if err != nil {
		u.logError(err)

		return nil, ErrCouldNotRegisterTaskDefinition
	}

	return result.TaskDefinition, nil
}

// RegisterTaskDefinitionWithEnvVars takes a task definition as an argument and updates its
// ContainerDefinitions field which contains environment variables
func (u *UFO) RegisterTaskDefinitionWithEnvVars(t *ecs.TaskDefinition) (*ecs.TaskDefinition, error) {
	result, err := u.ECS.RegisterTaskDefinition(&ecs.RegisterTaskDefinitionInput{
		Cpu:                     t.Cpu,
		Family:                  t.Family,
		Memory:                  t.Memory,
		Volumes:                 t.Volumes,
		NetworkMode:             t.NetworkMode,
		ExecutionRoleArn:        t.ExecutionRoleArn,
		TaskRoleArn:             t.TaskRoleArn,
		ContainerDefinitions:    t.ContainerDefinitions,
		RequiresCompatibilities: t.RequiresCompatibilities,
	})

	if err != nil {
		u.logError(err)
		return nil, ErrCouldNotRegisterTaskDefinition
	}

	return result.TaskDefinition, nil
}

// UpdateTaskDefinitionImage copies a task definition and update its image tag
func (u *UFO) UpdateTaskDefinitionImage(t ecs.TaskDefinition, version string) ecs.TaskDefinition {
	r := regexp.MustCompile(`(\S+):`)
	currentImage := *t.ContainerDefinitions[0].Image

	repo := r.FindStringSubmatch(currentImage)[1]
	newImage := fmt.Sprintf("%s:%s", repo, version)

	*t.ContainerDefinitions[0].Image = newImage

	return t
}

// GetRepoFromImage parses an image URL:tag and reads its repo
func (u *UFO) GetRepoFromImage(image *string) string {
	// Parse the repo  out of an image tag
	r := regexp.MustCompile(`\/(\S+):`) // /repo:sha256:
	repo := r.FindStringSubmatch(*image)[1]

	return repo
}

// UpdateService updates a service in a cluster with a new task definition
func (u *UFO) UpdateService(c *ecs.Cluster, s *ecs.Service, t *ecs.TaskDefinition) (*ecs.UpdateServiceOutput, error) {
	result, err := u.ECS.UpdateService(&ecs.UpdateServiceInput{
		Cluster:        c.ClusterArn,
		Service:        s.ServiceArn,
		TaskDefinition: t.TaskDefinitionArn,
	})

	if err != nil {
		u.logError(err)

		return nil, ErrCouldNotUpdateService
	}

	return result, nil
}

// Deploy a version to a service in a cluster
func (u *UFO) Deploy(c *ecs.Cluster, s *ecs.Service, version string) (*ecs.TaskDefinition, error) {
	t, err := u.RegisterTaskDefinitionWithImage(c, s, version)

	if err != nil {
		u.logError(err)

		return nil, err
	}

	_, err = u.UpdateService(c, s, t)

	if err != nil {
		u.logError(err)

		return nil, err
	}

	return t, err
}

// RunTask runs a specified task in a cluster
func (u *UFO) RunTask(c *ecs.Cluster, t *ecs.TaskDefinition, cmd string) (*ecs.RunTaskOutput, error) {
	splitString := strings.Split(cmd, " ")

	result, err := u.ECS.RunTask(&ecs.RunTaskInput{
		Cluster:        c.ClusterName,
		TaskDefinition: t.TaskDefinitionArn,
		Overrides: &ecs.TaskOverride{
			ContainerOverrides: []*ecs.ContainerOverride{&ecs.ContainerOverride{
				Command: aws.StringSlice(splitString),
				Name:    t.ContainerDefinitions[0].Name,
			}},
		},
	})

	if err != nil {
		u.logError(err)

		return nil, ErrCouldNotRunTask
	}

	return result, nil
}

// IsServiceRunning is meant to be called after a service update. This function checks if the newly
// started task has the status "RUNNING"
func (u *UFO) IsServiceRunning(c *ecs.Cluster, s *ecs.Service, t *ecs.TaskDefinition) (bool, error) {
	if *s.DesiredCount <= 0 {
		return false, nil
	}

	runningTasks, err := u.RunningTasks(c, s)

	if err != nil {
		u.logError(err)
		return false, err
	}

	if len(runningTasks) <= 0 {
		return false, nil
	}

	tasks, err := u.GetTasks(c, runningTasks)

	if err != nil {
		u.logError(err)
		return false, err
	}

	for _, task := range tasks {
		if *task.TaskDefinitionArn == *t.TaskDefinitionArn && *task.LastStatus == "RUNNING" {
			return true, nil
		}
	}

	return false, nil
}
