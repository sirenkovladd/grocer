import van from "vanjs-core"
import { api, navigate } from "../main"

const { div, form, input, button, label, h1, p } = van.tags

const Login = () => {
  const username = van.state("")
  const password = van.state("")
  const error = van.state("")

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    error.val = ""

    try {
      // The server authenticates by setting an HttpOnly session cookie
      // via the Set-Cookie response header. The browser stores and
      // auto-attaches it; we don't read the token out of the response.
      await api.post("/auth/login", {
        username: username.val,
        password: password.val,
      })
      navigate("/receipts")
    } catch (err) {
      error.val = "Login failed"
    }
  }

  return div({ class: "login-page" },
    form({ class: "login-form", onsubmit: handleSubmit },
      h1("Grocer"),
      () => error.val ? p({ class: "error" }, error.val) : "",
      div({ class: "form-group" },
        label({ for: "username" }, "Username"),
        input({
          id: "username",
          type: "text",
          value: username,
          oninput: (e: Event) => username.val = (e.target as HTMLInputElement).value,
        }),
      ),
      div({ class: "form-group" },
        label({ for: "password" }, "Password"),
        input({
          id: "password",
          type: "password",
          value: password,
          oninput: (e: Event) => password.val = (e.target as HTMLInputElement).value,
        }),
      ),
      button({ type: "submit" }, "Login"),
    ),
  )
}

export default Login
