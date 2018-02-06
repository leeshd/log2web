# log2web

- how to useage: 
> {your_process} 2>&1 | log2web -h={your_hostname} -p={your_port} -w={your_external_ws_uri}"
- example: 
> ./mytest 2>&1 | log2web -h=localhost -p=6699 -w=ws://localhost:6699/logws"