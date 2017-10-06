package main

import (
	"regexp"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ecr"
)

// ECRService ...
var ECRService *ecr.ECR

func getStageDigest(images []*ecr.ImageIdentifier) string {
	const STAGE = "latest"
	var latestDigest string
	for _, image := range images {
		if *image.ImageTag == STAGE {
			latestDigest = *image.ImageDigest
		}
	}
	return latestDigest
}

func describeImages(cluster string, service string) []*ecr.ImageDetail {
	currentTaskDefinition := describeTaskDefinition(cluster, service)
	currentImage := *currentTaskDefinition.ContainerDefinitions[0].Image
	r := regexp.MustCompile(`\/(\S+):`)
	repoName := r.FindStringSubmatch(currentImage)[1]
	input := &ecr.DescribeImagesInput{
		RepositoryName: aws.String(repoName),
	}

	result, err := ECRService.DescribeImages(input)
	handleECRErr(err)

	images := make([]*ecr.ImageDetail, 0)
	for _, image := range result.ImageDetails {
		if image.ImageTags != nil {
			images = append(images, image)
		}
	}

	return images
}

func filterImages(images []*ecr.ImageDetail) []string {
	versions := make([]string, 0)
	for _, image := range images {
		versions = append(versions, *image.ImageTags[0])
	}
	return versions
}

func listRepos() []*ecr.Repository {
	input := &ecr.DescribeRepositoriesInput{}

	result, err := ECRService.DescribeRepositories(input)
	handleECRErr(err)

	return result.Repositories
}
