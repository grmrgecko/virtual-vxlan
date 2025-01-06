FROM debian:bookworm

ARG PCAP_VER=1.10.5

RUN apt-get update; \
    apt-get --no-install-recommends -y -q install \
        build-essential \
        flex \
        bison \
        ca-certificates \
        wget

RUN wget http://www.tcpdump.org/release/libpcap-${PCAP_VER}.tar.gz && \
    tar xvf libpcap-${PCAP_VER}.tar.gz && \
    cd libpcap-${PCAP_VER} && \
    ./configure --with-pcap=linux --prefix=/usr/ \
    --disable-usb --disable-bluetooth --disable-dbus --disable-rdma --disable-shared && \
    make && \
    make install && \
    cd ../ && \
    rm -Rf libpcap-${PCAP_VER}
