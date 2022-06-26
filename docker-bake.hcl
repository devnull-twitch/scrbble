target "build" {
    inherits = ["docker-metadata-action"]
    dockerfile = "Dockerfile"
}