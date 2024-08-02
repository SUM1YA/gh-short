# Define variables
IMAGE_NAME=ttl.sh/github-short
TAG=latest

# Targets
.PHONY: build

# Build the Docker image
build:
	docker build -t $(IMAGE_NAME):$(TAG) .

# Run the Docker container
run:
	docker run --rm -p 8080:8080 $(IMAGE_NAME):$(TAG)

# Remove the Docker image
clean:
	docker rmi $(IMAGE_NAME):$(TAG)

# Build and run the Docker container
all: build run
