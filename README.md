# Easy Transcoder

A simple yet powerful static transcoding server that leverages FFmpeg to transcode video files according to customizable profiles.

## Overview

Easy Transcoder is a web-based application that provides a user-friendly interface for video transcoding tasks. It uses FFmpeg to convert video files to different formats and resolutions based on user-defined profiles in the configuration.

## Features

- **Web Interface**: Simple and intuitive UI for managing transcoding tasks
- **Profile System**: Create and use multiple transcoding profiles with customizable FFmpeg parameters
- **Queue Management**: Organize and monitor transcoding jobs with progress tracking
- **File Browser**: Easily select files for transcoding through the built-in file browser
- **Task Resolution**: Choose whether to replace original files or save as new files

## Architecture

- Built in Go with a modern web interface
- Uses FFmpeg for transcoding operations
- Implements a queue-based processing system for handling multiple tasks
- Supports custom transcoding profiles via configuration

## Installation

### Prerequisites

- Go 1.18 or higher
- FFmpeg installed on your system

### Building from source

```bash
# Clone the repository
git clone https://github.com/yourusername/easy-transcoder.git
cd easy-transcoder

# Build the project
make build
```

## Usage

1. Configure your profiles in `config.yaml` (see Configuration section)
2. Start the server:
   ```bash
   ./bin/easy-transcoder
   ```
3. Access the web interface at `http://localhost:8080`
4. Select a file, choose a transcoding profile, and submit the task
5. Monitor the transcoding progress in the queue view

## Configuration

Create a `config.yaml` file with your transcoding profiles:

```yaml
tempdir: "/path/to/temp/directory" # Temporary directory for in-progress transcodes

profiles:
  - name: "x264-high"
    params:
      c:v: "libx264"
      preset: "slow"
      crf: "18"
      c:a: "aac"
      b:a: "128k"

  - name: "x265-medium"
    params:
      c:v: "libx265"
      preset: "medium"
      crf: "23"
      c:a: "aac"
      b:a: "128k"
```

Each profile contains a name and a map of FFmpeg parameters that will be passed to the transcoder.

## Docker

A Dockerfile is provided for containerized deployment:

```bash
# Build the Docker image
docker build -t easy-transcoder .

# Run the container
docker run -p 8080:8080 -v /path/to/media:/media -v /path/to/config:/app/config easy-transcoder
```

### TODO

- [x] Queue
- [x] Profiles
- [x] Task cancel
- [ ] Better resolution UI
- [x] VMAF
- [ ] Browser Notifications
- [x] ffmpeg version check
- [ ] download custom ffmpeg binary
- [x] cpu usage
- [ ] SSE for queue updates
- [ ] task mutiprocessing
