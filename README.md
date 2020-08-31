# cliptun

[cliptun](https://github.com/svent/cliptun) allows to tunnel through synchronized clipboards. Anywhere you can copy & paste data between two systems, you can use cliptun: RDP connections with clipboard synchronization, virtual machines with clipboard synchronization between guest and host system or the new cloud-based clipboard synchronization in Windows 10.

These clipboard synchronizations can even be "chained": if a Linux host runs a Windows VM with clipboard synchronization and an RDP connection from that VM to a Windows server is established, cliptun running on the Linux host can directly communicate with the remote Windows server.


## Supported Modes

cliptun allows to connect to a remote shell (like bash or cmd.exe), transfer files (using a built-in SFTP server) or tunnel multiple network connections (including a built-in SOCKS5 proxy).

cliptun creates a virtual channel between its client and server part, sending and receiving data in compressed, encrypted, authenticated (if you specify a custom password) and base64 encoded chunks, transferred by writing to and reading from the local clipboard (which is then synchronized via remote desktop protocol).

As the clipboard is a shared medium without any support for one side to detect collisions when writing to it, cliptun emulates something similar to a TCP connection by using the same SYN/ACK mechanism to detect collisions and enable resynchronization attempts (including random delays, an idea borrowed from early ethernet technology).

<img src="https://svent.dev/img/cliptun-stdio.svg"></img>
<br />
cliptun supports three "client" modes (readline, stdin and client) and three "server" modes (exec, stdout and server).

### readline + exec
read commands on one system and execute them via shell on the other system
```plain
# System 1
cliptun.exe exec cmd.exe
# System 2
cliptun.exe readline
```

### stdin + stdout
transfer raw data
<br />
```plain
# System 1
cliptun.exe stdout >large-file
# System 2
cliptun.exe stdin <large-file
```

### client + server
cliptun enters a dynamic shell, allowing to dynamically add port fowardings, start a socks server, or enter an SFTP mode for uploading or downloading files.
```plain
# System 1
cliptun.exe server
# System 2
cliptun.exe client --fwd-local 3000:localhost:3000 --socks 1080
```

### Demo of client + server mode                                                               

The following video shows two CLI windows: the left one runs on a local machine, the right one runs on an AWS EC2 instance, connected through RDP.
            
cliptun first connects a readline interface to a cmd.exe running on the remote machine. In the second part the client+server mode is used to list the remote directory, upload a file and execute it.

(Please see the [project site](https://svent.dev/projects/cliptun/) for a high resolution video)

<img width="100%" src="https://svent.dev/img/cliptun-demo.gif"></img>


## Options
The two most important options are ```--blocksize``` and ```--interval```.

Blocksize specifies how much data is read for one chunk transferred through the clipboard. This is the raw size of data read, i.e. the chunk written to the clipboard might be larger due to the base64 encoding. It defaults to 64k, which allows to tunnel through the Windows 10 clipboard synchronization (limited to 100k for text as far as I know).

Interval speficies how long cliptun waits between reading (and writing) the clipboard. It currently defaults to 1 second, optimizing more for stability than for performance.

These two options allow tuning the connection and more aggressive values might work quite well. If you see messages like ```Error: out of sync, trying to resync...``` the connection is too slow and you should increase the interval or decrease the blocksize.

The option ```--password``` allows to set a custom password (of course this must be the same on both sides). The password is used to derive an encryption key via PBKDF2, which is used to encrypt (and authenticate) the transferred chunks via XSalsa20 and Poly1305, implemented by using the NaCl secretbox implementation for Go. By default the password is set to "cliptun".

The option ```--transfer``` allows to transfer data via other mechanisms than the clipboard. This can be used to take advantage of cliptun's advanced tunneling capabilities (like shell execution or file transfer) over other transports like a simple tcp connection (that might be provided by another tunneling tool) or by executing other programs.

### Example, using the external program netcat as a transport mechanism
```plain
# System 1
./cliptun --transport "exec=nc -l -p 3000" exec /bin/bash
# System 2
./cliptun --transport "exec=nc 10.1.2.3 3000" readline
```


### Example, using a tcp connection as a transport mechanism
```plain
# System 1
./cliptun --transport "tcp-listen=:5000" --interval 100ms --blocksize 256k server
# System 2
./cliptun --transport "tcp=10.1.2.3:5000" --interval 100ms --blocksize 256k client
```

## Installation

Binaries for Windows and Linux are automatically generated for every new version as part of the GitHub [releases](https://github.com/svent/cliptun/releases).

Alternatively, the project can be compiled by cloning this repository and executing ```go build```.

On Linux systems, xclip should be installed for accessing the clipboard.

The tool can be tested by running two instances on the same machine.

