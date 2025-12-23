## hue-control-tui

### About
This project is aimed at creating a useful TUI for Philips Hue lights.

It utilizes the openhue-go library https://github.com/openhue/openhue-go to interact with the lights using the Philips Hue Bridge.

### Features

- 🎨 **Interactive TUI** - Beautiful terminal interface with real-time updates
- 💡 **Light Control** - Toggle, adjust brightness, and manage multiple lights
- 🔌 **Connectivity Detection** - Shows unreachable lights instantly
- ⚡ **Real-time Updates** - SSE integration for immediate state changes
- 🎬 **Scene Control** - Activate Hue scenes via commands
- ⌨️ **Keyboard Navigation** - Vim-style keybindings for efficiency

### Install

Clone the repository and run `go build` to build the binary.

```bash
git clone <repository-url>
cd hue-control-tui
go build
./hue-control-tui
```

### Setup

Initial execution will prompt the user to click the button on their Philips Hue Bridge if one is found.

The Bridge IP and authentication header will be stored in a configuration file in the `~/.openhue` directory and will be read on subsequent executions.

You also have the ability to start the program using your own configuration file using the `--bridge_ip` and `--key` flags:

```bash
./hue-control-tui --bridge_ip 192.168.1.100 --key your-api-key-here
```

Use the `--debug` flag to enable debug logging at startup. It will create a debug.log file in the `~/.openhue` directory.

```bash
./hue-control-tui --debug
```

### Usage

#### Keyboard Controls
- **Space** - Select/deselect light
- **Enter** - Toggle selected lights on/off
- **← / h** - Decrease brightness
- **→ / l** - Increase brightness
- **↑ / k** - Move cursor up
- **↓ / j** - Move cursor down
- **:** - Open command mode
- **q** - Quit

#### Commands
- `:help` - Show available commands
- `:refresh` - Refresh lights and check connectivity
- `:all_on` - Turn all reachable lights on
- `:all_off` - Turn all reachable lights off
- `:scene <name>` - Activate a scene

### Unreachable Light Detection

The TUI now displays connectivity status for all lights. Lights that are powered off, unplugged, or disconnected from the Zigbee network will be marked as **UNREACHABLE** with orange text, and their brightness will show as **N/A**.

Unreachable lights are automatically skipped when attempting to control them. Use the `:refresh` command to update connectivity status.

For detailed information, see [UNREACHABLE_LIGHTS.md](UNREACHABLE_LIGHTS.md).

### More

Current plans include adding support for Rooms and Light Groups.

Also, it would be interesting to add support for the waybar module.

### Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

### License

See LICENSE file for details.
