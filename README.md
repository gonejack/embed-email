# embed-email
This command parse email content, download remote images and replace them as inline images, then you can import these fat emails to regular mail clients like Microsoft Outlook or Apple Mail to send them.

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/gonejack/embed-email)
![Build](https://github.com/gonejack/embed-email/actions/workflows/go.yml/badge.svg)
[![GitHub license](https://img.shields.io/github/license/gonejack/embed-email.svg?color=blue)](LICENSE)

### Install
```shell
> go get github.com/gonejack/embed-email
```

### Usage
```shell
> embed-email *.eml
```
```
Usage: embed-email [<eml> ...]

Command line tool for embed images within email.

Arguments:
  [<eml> ...]

Flags:
  -h, --help          Show context-sensitive help.
      --retain-gif    will not convert gif into mp4.'
  -v, --verbose       Verbose printing.
```
