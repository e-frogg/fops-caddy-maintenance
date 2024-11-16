# Variables
BINARY_NAME=caddy
BUILD_DIR=build
XCADDY=xcaddy
MODULE_PATH=github.com/e-frogg/frops-caddy-maintenance

# Colors for terminal output
GREEN=\033[0;32m
NC=\033[0m # No Color

.PHONY: all clean build run

all: clean build

build:
	@echo "${GREEN}Building Caddy with maintenance plugin...${NC}"
	@mkdir -p $(BUILD_DIR)
	@$(XCADDY) build \
		--with $(MODULE_PATH)=. \
		--output $(BUILD_DIR)/$(BINARY_NAME)
	@echo "${GREEN}Build complete! Binary located at $(BUILD_DIR)/$(BINARY_NAME)${NC}"

run: build
	@echo "${GREEN}Running Caddy with debug mode...${NC}"
	@sudo DEBUG=1 ./$(BUILD_DIR)/$(BINARY_NAME) run --config ./build/Caddyfile

clean:
	@echo "${GREEN}Cleaning build...${NC}"
	@rm -rf $(BUILD_DIR)/caddy