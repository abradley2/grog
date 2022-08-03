# Grog

Pipe [ndjson](http://ndjson.org/) logs from `stdout` and `stderr` to a temporary 
[embedded key-value database](https://github.com/etcd-io/bbolt)
and serve a UI to browse through them

> Grog is a secret mixture that contains one or more of the following:  
...Kerosene  
...Propylene Glycol  
...Artificial Sweetener  
...Sulfuric Acid  
...Rum  
...Acetone  
...Battery Acid  
...Red dye #2  
...Scummmmm  
...Axle grease and/or pepperoni

# Installation

`go get github.com/abradley2/grog`

# Usage

```
start someprocess | PORT=9966 grog
```

The embedded database file will be created on process start and removed on process exit.
