#!/bin/bash
set -e

# Create build directory if it doesn't exist
mkdir -p build

echo "Building mgrok server..."
go build -o build/mgrok-server ./cmd/server

echo "Building mgrok client..."
go build -o build/mgrok-client ./cmd/client

echo "Build completed successfully!"
echo "Binaries available in the build/ directory" 