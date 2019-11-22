FROM golang:1.13.4

ENV VERSION 1.0

WORKDIR /workdir

# for those in China
#RUN mv /etc/apt/sources.list /etc/apt/sources.list.bak && \
#    echo "deb http://mirrors.163.com/debian/ buster main non-free contrib" >/etc/apt/sources.list && \
#    echo "deb http://mirrors.163.com/debian/ buster-proposed-updates main non-free contrib" >>/etc/apt/sources.list && \
#    echo "deb-src http://mirrors.163.com/debian/ buster main non-free contrib" >>/etc/apt/sources.list && \
#    echo "deb-src http://mirrors.163.com/debian/ buster-proposed-updates main non-free contrib" >>/etc/apt/sources.list


RUN apt update -y && apt install -y llvm libclang-dev mesa-opencl-icd ocl-icd-opencl-dev

RUN mkdir /workdir/src

COPY go.mod go.sum src/

COPY extern/ src/extern/

# for those in China
#RUN go env -w GOPROXY=https://goproxy.cn,direct
#RUN go env -w GOSUMDB="sum.golang.google.cn"

RUN cd src &&  go mod download

    
COPY . src

RUN cd src && make && cp lotus lotus-storage-miner ../

RUN rm -rf src

CMD ["/workdir/src/lotus","daemon"]