import { useTranslation } from "react-i18next";
import { motion } from "framer-motion";

export default function DrawingMain() {
  const { t } = useTranslation();

  return (
    <motion.div
      className="drawing-main"
      initial={{ opacity: 0, x: 24 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.35, ease: "easeOut" }}
    >
      <div className="drawing-main-placeholder">
        <p className="drawing-main-title">{t("drawing.mainTitle")}</p>
        <p className="drawing-main-desc">{t("drawing.mainDesc")}</p>
      </div>
    </motion.div>
  );
}
