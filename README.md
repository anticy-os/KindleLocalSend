# LocalSend Kindle

A small Go-based LocalSend receiver for Kindle devices.

Tested only on Kindle Oasis 8th gen.

## Requirements

- Go 1.20+ installed
- `make` installed
- Kindle with Jailbreak
- KUAL installed on Kindle

## Build

From the repository root:

```sh
make build
```

## Install

Copy the `localsend` folder to your Kindle

```sh
cp -r localsend /mnt/us/extensions/
```
