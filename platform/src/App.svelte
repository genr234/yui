<script lang="ts">
import { Bridge } from './sdk/bridge'

const bridge = new Bridge()

// test round-trip
bridge.send("ping", { hello: "world" }).then(console.log)
</script>