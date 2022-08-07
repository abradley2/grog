const fs = require('fs')

const logsPerSecond = 1

setInterval(function () {
    const d = new Date()
    const a = Intl.DateTimeFormat('en-US', { dateStyle: 'long', timeStyle: 'long' }).format(d)
    console.log(`${a}, ${d.getMilliseconds()}ms`)
}, 1000 / logsPerSecond)