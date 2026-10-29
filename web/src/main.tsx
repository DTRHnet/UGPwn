import { render } from "preact";
import "./tokens.css";
import "./app.css";
import { App } from "./App";

const root = document.getElementById("app");
if (root !== null) {
  render(<App />, root);
}
