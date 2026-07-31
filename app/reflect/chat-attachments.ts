export interface ChatAttachment {
  id: string;
  name: string;
  mediaType: string;
  dataUrl: string;
}

const MAX_IMAGE_EDGE = 1568;
const JPEG_QUALITY = 0.85;
const PROVIDER_SAFE_TYPES = new Set([
  "image/jpeg",
  "image/png",
  "image/webp",
  "image/gif",
]);

export function imageFilesFrom(data: DataTransfer | null): File[] {
  return data
    ? Array.from(data.files).filter((file) => file.type.startsWith("image/"))
    : [];
}

async function downscaledImage(
  file: File,
): Promise<{ dataUrl: string; mediaType: string } | null> {
  if (typeof createImageBitmap !== "function") return null;
  let bitmap: ImageBitmap;
  try {
    bitmap = await createImageBitmap(file);
  } catch {
    return null;
  }
  try {
    const scale = Math.min(
      MAX_IMAGE_EDGE / Math.max(bitmap.width, bitmap.height, 1),
      1,
    );
    if (scale === 1 && PROVIDER_SAFE_TYPES.has(file.type)) return null;
    const canvas = document.createElement("canvas");
    canvas.width = Math.max(1, Math.round(bitmap.width * scale));
    canvas.height = Math.max(1, Math.round(bitmap.height * scale));
    const context = canvas.getContext("2d");
    if (!context) return null;
    context.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
    return {
      dataUrl: canvas.toDataURL("image/jpeg", JPEG_QUALITY),
      mediaType: "image/jpeg",
    };
  } finally {
    bitmap.close();
  }
}

function fileDataUrl(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.addEventListener("load", () => resolve(String(reader.result)));
    reader.addEventListener("error", () =>
      reject(reader.error ?? new Error("无法读取图片")),
    );
    reader.readAsDataURL(file);
  });
}

export async function toChatAttachment(file: File): Promise<ChatAttachment> {
  const downscaled = await downscaledImage(file);
  return {
    id: crypto.randomUUID(),
    name: file.name,
    mediaType: downscaled?.mediaType ?? file.type,
    dataUrl: downscaled?.dataUrl ?? (await fileDataUrl(file)),
  };
}
