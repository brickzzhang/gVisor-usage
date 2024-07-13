# gVisor_usage

This project shows two ways to use gVisor/Network as network stack, to implement a simple http server. One is using go channel as link layer device, the other is using unix file as link layer device.

For official gVisor/Network example, please refer to [gVisor/tcpip/sample](https://github.com/google/gvisor/tree/master/pkg/tcpip/sample)

# How to run

## Using fd as link layer device

1. create a tun device, and enable

```bash
ip tuntap add mode tun tun0
ip addr add 192.168.0.96/24 dev tun0
ip link set tun0 up
```

2. build http server

```bash
gox -osarch="linux/amd64" -gcflags="all=-l -N" -output fddev.bin ./fddev
```

3. run http server

```bash
./fddev.bin tun0 192.168.0.100 6100
```

4. make a http request on client side

```bash
curl -kv http://192.168.0.100:6100
```

The output is as below:

```bash
~ curl -kv http://192.168.0.100:6100
*   Trying 192.168.0.100:6100...
* Connected to 192.168.0.100 (192.168.0.100) port 6100 (#0)
> GET / HTTP/1.1
> Host: 192.168.0.100:6100
> User-Agent: curl/7.74.0
> Accept: */*
>
* Mark bundle as not supporting multiuse
< HTTP/1.1 200 OK
< Date: Sat, 13 Jul 2024 06:45:19 GMT
< Content-Length: 25
< Content-Type: text/plain; charset=utf-8
<
* Connection #0 to host 192.168.0.100 left intact
Hello kung fu developer! #
```

## Using go channel as link layer device

To extend the scenario, we will use `unix domain socket` as undelay device linked to `gVisor go chan endpoint`. The Arch is like below:

![go-chan-arch](https://github.com/brickzzhang/gVisor-usage/blob/main/go-chan-arch.png)

1. create a tun device, and enable

```bash
ip tuntap add mode tun tun0
ip addr add 192.168.0.96/24 dev tun0
ip link set tun0 up
```

2. build http server

```bash
gox -osarch="linux/amd64" -gcflags="all=-l -N" -output gochan.bin ./chandev/gvisorhttp
```

3. build echo proxy

```bash
gox -osarch="linux/amd64" -gcflags="all=-l -N" -output echo.bin ./chandev/echo
```

4. run http server

```bash
./gochan.bin 192.168.0.100 6100
```

5. run echo proxy

```bash
./echo.bin tun0
```

6. make a http request on client side

```bash
curl -kv http://192.168.0.100:6100
```

The output is as below:

```bash
~ curl -kv http://192.168.0.100:6100
*   Trying 192.168.0.100:6100...
* Connected to 192.168.0.100 (192.168.0.100) port 6100 (#0)
> GET / HTTP/1.1
> Host: 192.168.0.100:6100
> User-Agent: curl/7.74.0
> Accept: */*
>
* Mark bundle as not supporting multiuse
< HTTP/1.1 200 OK
< Date: Sat, 13 Jul 2024 07:45:16 GMT
< Content-Length: 25
< Content-Type: text/plain; charset=utf-8
<
* Connection #0 to host 192.168.0.100 left intact
Hello kung fu developer! #
```
