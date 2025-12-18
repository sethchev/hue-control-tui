## hue-control-tui

### About
This project is aimed at creating a useful TUI for Philips Hue lights.

It utilizes the openhue-go library https://github.com/openhue/openhue-go to interact with the lights using the Philips Hue Bridge.

### Setup

Initial execution will prompt the user to click the button on their Philips Hue Bridge if one is found.

The Bridge IP and authentication header will be stored in a configuration file in the ~/.openhue directory and will be read on subsequent executions.

You also have the ability to start the program using your own configuration file using the bridge_ip and key flags.

### More

This project is in its early stages and could change drastically.
