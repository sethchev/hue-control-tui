## hue-control-tui

### About
This project is aimed at creating a useful TUI for Philips Hue lights.

It utilizes the openhue-go library https://github.com/openhue/openhue-go to interact with the lights using the Philips Hue Bridge.

### Install

Clone the repository and run `go build -o hue-control-tui` to build the binary.

### Setup

Initial execution will prompt the user to click the button on their Philips Hue Bridge if one is found.

The Bridge IP and authentication header will be stored in a configuration file in the ~/.openhue directory and will be read on subsequent executions.

You also have the ability to start the program using your own configuration file using the bridge_ip and key flags.

Use the --debug flag to enable debug logging at startup. It will create a debug.log file in the ~/.openhue directory.

### More

Current plans include adding support for Rooms and Light Groups.

Also, it would be interesting to add support for the waybar module.
