export type ToastType = "success" | "error" | "info";

export type ToastPayload = {
  message: string;
  type?: ToastType;
  duration?: number;
};

export const toastEvent = "productserver:toast";

export function toast(message: string, type: ToastType = "success", duration = 2800) {
  window.dispatchEvent(new CustomEvent<ToastPayload>(toastEvent, { detail: { message, type, duration } }));
}
