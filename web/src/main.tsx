import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import "./index.css";
import "./i18n";
import App from "./App";
import { AuthProvider } from "./auth";
import { ThemeProvider } from "./theme";
import { ToastProvider } from "./components/ui/toast";

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <ThemeProvider>
    <ToastProvider>
      <BrowserRouter>
        <AuthProvider>
          <App />
        </AuthProvider>
      </BrowserRouter>
    </ToastProvider>
    </ThemeProvider>
  </StrictMode>,
);
