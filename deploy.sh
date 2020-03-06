docker build . -t registry.gitlab.com/kamackay/dns:$1 && \
    docker push registry.gitlab.com/kamackay/dns:$1 && \
    kubectl --context do-nyc3-keithmackay-cluster -n dns set image ds/dns server=registry.gitlab.com/kamackay/dns:$1
