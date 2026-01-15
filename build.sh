#!/bin/bash

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== CoAI Build Script ===${NC}"

# Check if Node.js is installed
if ! command -v node &> /dev/null; then
    echo -e "${RED}Error: Node.js is not installed${NC}"
    exit 1
fi

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go is not installed${NC}"
    exit 1
fi

# Build frontend
echo -e "${YELLOW}Building frontend...${NC}"
cd app
npm install --legacy-peer-deps
npm run build
cd ..

# Build backend
echo -e "${YELLOW}Building backend...${NC}"
go mod download
go build -ldflags="-s -w" -o coai .

echo -e "${GREEN}Build completed successfully!${NC}"
echo -e "${GREEN}Binary: ./coai${NC}"
echo -e "${GREEN}Frontend: ./app/dist${NC}"
