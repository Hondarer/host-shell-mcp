/* Linux/glibc getaddrinfo adapter. Wire families and errors are platform independent. */
#define _GNU_SOURCE
#include <arpa/inet.h>
#include <dlfcn.h>
#include <errno.h>
#include <netdb.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/time.h>
#include <sys/un.h>
#include <unistd.h>
#ifndef __GLIBC__
#error remote_dns_requires_glibc
#endif

const int host_shell_dns_adapter_v1 = 1;

static int (*real_gai)(const char *, const char *, const struct addrinfo *, struct addrinfo **);
static pthread_once_t once = PTHREAD_ONCE_INIT;
static void load_real(void) { *(void **)(&real_gai) = dlsym(RTLD_NEXT, "getaddrinfo"); }

static int query(const char *host, int family, char *reply, size_t capacity) {
    const char *endpoint = getenv("HOST_SHELL_DNS_SOCKET");
    struct sockaddr_un address = {.sun_family = AF_UNIX};
    if (!endpoint || strlen(endpoint) >= sizeof(address.sun_path)) return -1;
    strcpy(address.sun_path, endpoint);
    int fd = socket(AF_UNIX, SOCK_STREAM | SOCK_CLOEXEC, 0);
    if (fd < 0) return -1;
    struct timeval timeout = {.tv_sec = 4};
    setsockopt(fd, SOL_SOCKET, SO_RCVTIMEO, &timeout, sizeof(timeout));
    setsockopt(fd, SOL_SOCKET, SO_SNDTIMEO, &timeout, sizeof(timeout));
    char request[280];
    int length = snprintf(request, sizeof(request), "%d %s\n", family, host);
    int ok = length > 0 && (size_t)length < sizeof(request) && connect(fd, (struct sockaddr *)&address, sizeof(address)) == 0;
    for (int sent = 0; ok && sent < length;) {
        ssize_t n = send(fd, request + sent, (size_t)(length - sent), MSG_NOSIGNAL);
        if (n < 0 && errno == EINTR) continue;
        if (n <= 0) { ok = 0; break; }
        sent += (int)n;
    }
    size_t used = 0;
    while (ok && used + 1 < capacity) {
        ssize_t n = recv(fd, reply + used, capacity - used - 1, 0);
        if (n < 0 && errno == EINTR) continue;
        if (n < 0) { ok = 0; break; }
        if (n == 0) break;
        used += (size_t)n;
    }
    if (used + 1 == capacity) ok = 0;
    if (used >= capacity) { close(fd); return -1; }
    reply[used] = 0;
    close(fd);
    return ok && used ? 0 : -1;
}

int getaddrinfo(const char *node, const char *service, const struct addrinfo *hints, struct addrinfo **result) {
    pthread_once(&once, load_real);
    if (!real_gai) return EAI_SYSTEM;
    unsigned char numeric[sizeof(struct in6_addr)];
    const char *endpoint = getenv("HOST_SHELL_DNS_SOCKET");
    if (!node || !*node || !endpoint || !*endpoint ||
        (hints && (hints->ai_flags & AI_NUMERICHOST)) ||
        inet_pton(AF_INET, node, numeric) == 1 || inet_pton(AF_INET6, node, numeric) == 1 ||
        strcasecmp(node, "localhost") == 0)
        return real_gai(node, service, hints, result);
    if (strlen(node) > 253 || strpbrk(node, " \t\r\n")) return EAI_NONAME;
    *result = NULL;
    struct addrinfo settings = hints ? *hints : (struct addrinfo){0};
    if (settings.ai_family != AF_UNSPEC && settings.ai_family != AF_INET && settings.ai_family != AF_INET6) return EAI_FAMILY;
    char reply[8192];
    if (query(node, settings.ai_family == AF_INET ? 4 : settings.ai_family == AF_INET6 ? 6 : 0, reply, sizeof(reply)) != 0) return EAI_AGAIN;
    if (getenv("HOST_SHELL_DNS_TRACE")) fprintf(stderr, "host-shell-dns: getaddrinfo %s family=%d\n", node, settings.ai_family);
    if (strncmp(reply, "ERR NONAME", 10) == 0) return EAI_NONAME;
    if (strncmp(reply, "ERR AGAIN", 9) == 0) return EAI_AGAIN;
    if (strncmp(reply, "ERR FAMILY", 10) == 0) return EAI_FAMILY;
    int count;
    char canonical[254], *save = NULL;
    char *line = strtok_r(reply, "\n", &save);
    if (!line || sscanf(line, "OK %d %253s", &count, canonical) != 2 || count < 1 || count > 64) return EAI_FAIL;
    int canon = settings.ai_flags & AI_CANONNAME;
    settings.ai_flags = (settings.ai_flags & ~AI_CANONNAME) | AI_NUMERICHOST;
    struct addrinfo **tail = result;
    int last = EAI_NONAME;
    for (int i = 0; i < count; ++i) {
        line = strtok_r(NULL, "\n", &save);
        int family; char ip[INET6_ADDRSTRLEN];
        if (!line || sscanf(line, "%d %45s", &family, ip) != 2 || (family != 4 && family != 6) ||
            inet_pton(family == 4 ? AF_INET : AF_INET6, ip, numeric) != 1) {
            freeaddrinfo(*result); *result = NULL; return EAI_FAIL;
        }
        struct addrinfo *part = NULL;
        last = real_gai(ip, service, &settings, &part);
        if (last != 0) continue;
        *tail = part;
        while (*tail) tail = &(*tail)->ai_next;
    }
    if (!*result) return last;
    if (canon) {
        (*result)->ai_canonname = strdup(canonical);
        if (!(*result)->ai_canonname) { freeaddrinfo(*result); *result = NULL; return EAI_MEMORY; }
    }
    return 0;
}
