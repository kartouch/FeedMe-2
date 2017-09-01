FROM centos:7
LABEL io.openshift.expose-services 8080/tcp:http
RUN yum makecache && yum update -y && \
    yum install -y redis sqlite3 && \
    rm -rf /var/cache/yum
RUN  mkdir -p /data && \
    groupadd feedme -g 33 && \
    useradd feedme -u 33 -g 33 -G feedme && \
    echo "feedme:feedme" | chpasswd && \
    test "$(id feedme)" = "uid=33(feedme) gid=33(feedme) groups=33(feedme)"
COPY feedme /home/feedme
RUN chown -R 33.33 /data
VOLUME ["/data"]
USER 33
CMD ["/home/feedme/feedme"]
