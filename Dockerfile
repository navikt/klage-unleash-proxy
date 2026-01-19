FROM scratch

COPY server /server

EXPOSE 8080

CMD ["/server"]
