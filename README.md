# Grog

Pipe [ndjson](http://ndjson.org/) or any newline-delimited format
logs from `stdout` and `stderr` to a temporary 
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

TODO

# Usage

```
start someprocess | PORT=9966 grog
```

The embedded database file will be created on process start and removed on process exit.

# Build Requirements

* [uglifyjs v3.x](https://github.com/mishoo/UglifyJS)
* [elm 0.19.1](https://elm-lang.org)
* [Go v1.19](https://go.dev/dl/)
