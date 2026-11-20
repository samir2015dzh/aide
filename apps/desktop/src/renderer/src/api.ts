import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";
import { LocalService } from "@aide/proto/local";

// aided's LocalService, through the main process (app://aide/api).
const transport = createConnectTransport({ baseUrl: "app://aide/api", useBinaryFormat: true });
export const local = createClient(LocalService, transport);
