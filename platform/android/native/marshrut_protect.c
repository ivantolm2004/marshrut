// Fail-closed socket protection bridge for the packaged tpws helper.
#include <errno.h>
#include <stddef.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

int marshrut_protect(int fd) {
    const char *name = getenv("MARSHRUT_PROTECT_SOCKET");
    if (!name || !*name || strlen(name) > 90) { errno = EPERM; return -1; }
    int channel = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (channel < 0) return -1;
    struct timeval timeout = { .tv_sec = 3, .tv_usec = 0 };
    setsockopt(channel, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout));
    setsockopt(channel, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof(timeout));
    struct sockaddr_un addr = { .sun_family = AF_UNIX };
    memcpy(addr.sun_path + 1, name, strlen(name));
    if (connect(channel, (struct sockaddr *)&addr, offsetof(struct sockaddr_un, sun_path) + 1 + strlen(name)) < 0) { close(channel); return -1; }
    unsigned char request = 1, response = 0;
    struct iovec iov = { .iov_base = &request, .iov_len = 1 };
    union { struct cmsghdr align; unsigned char bytes[CMSG_SPACE(sizeof(int))]; } control;
    memset(&control, 0, sizeof(control));
    struct msghdr message = { .msg_iov = &iov, .msg_iovlen = 1, .msg_control = control.bytes, .msg_controllen = sizeof(control.bytes) };
    struct cmsghdr *cmsg = CMSG_FIRSTHDR(&message);
    cmsg->cmsg_level = SOL_SOCKET; cmsg->cmsg_type = SCM_RIGHTS; cmsg->cmsg_len = CMSG_LEN(sizeof(int));
    memcpy(CMSG_DATA(cmsg), &fd, sizeof(int));
    int ok = sendmsg(channel, &message, MSG_NOSIGNAL) == 1 && recv(channel, &response, 1, 0) == 1 && response == 1;
    close(channel);
    if (!ok) { errno = EPERM; return -1; }
    return 0;
}
