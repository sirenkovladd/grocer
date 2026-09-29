import { createSignal, Show } from "solid-js"
import { api, navigate } from "../lib/api"

export default function Login() {
  const [username, setUsername] = createSignal("")
  const [password, setPassword] = createSignal("")
  const [error, setError] = createSignal("")

  const handleSubmit = async (e: Event) => {
    e.preventDefault()
    setError("")

    try {
      // The server authenticates by setting an HttpOnly session cookie
      // via the Set-Cookie response header. The browser stores and
      // auto-attaches it; we don't read the token out of the response.
      await api.post("/auth/login", {
        username: username(),
        password: password(),
      })
      navigate("/receipts")
    } catch {
      setError("Login failed")
    }
  }

  return (
    <div class="login-page">
      <form class="login-form" onSubmit={handleSubmit}>
        <h1>Grocer</h1>
        <Show when={error()}>
          <p class="error">{error()}</p>
        </Show>
        <div class="form-group">
          <label for="username">Username</label>
          <input
            id="username"
            type="text"
            value={username()}
            onInput={(e) => setUsername(e.currentTarget.value)}
          />
        </div>
        <div class="form-group">
          <label for="password">Password</label>
          <input
            id="password"
            type="password"
            value={password()}
            onInput={(e) => setPassword(e.currentTarget.value)}
          />
        </div>
        <button type="submit">Login</button>
      </form>
    </div>
  )
}
