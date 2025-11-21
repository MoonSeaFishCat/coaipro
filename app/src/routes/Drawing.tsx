import "@/assets/pages/chat.less";
import DrawingSidebar from "@/components/drawing/DrawingSidebar.tsx";
import DrawingMain from "@/components/drawing/DrawingMain.tsx";

function Drawing() {
  return (
    <div className="home-page flex flex-row flex-1">
      <DrawingSidebar />
      <DrawingMain />
    </div>
  );
}

export default Drawing;
