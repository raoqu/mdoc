import { ReflectWorkspace } from "./reflect/workspace";
import { localDateKey } from "./reflect/markdown";

export default function Home() {
  return <ReflectWorkspace initialDate={localDateKey()} />;
}
