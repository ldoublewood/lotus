
# Snark 外包使用

## DBC

miner上设置:
USE_SNARK_OUTSOURCING=_yes_

一台控制snark的worker机上设置：
USE_DBC_SNARK=_yes_
USE_DBC_SNARK=<可并行C2数量>

## CX
miner上设置:
USE_SNARK_OUTSOURCING=_yes_

一台控制snark的worker机上设置：
USE_CX_SNARK=_yes_
并在以下配置文件中配置远程信息：
/etc/cxsnark.json

{
  "SnarkUrls":[
    {
      "Path": "127.0.0.1:8855"
    }
  ]
}



