import { render } from "solid-js/web"
import App from "./App"

// Global stylesheet is linked from index.html
// (<link href="./styles/main.css">), which Vite bundles into dist.
// Keep that single path so class names stay byte-identical.

render(() => <App />, document.getElementById("app")!)
