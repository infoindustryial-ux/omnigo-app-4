# 🌐 OMNIGO Super App — Official Landing Page & Web Portal

A high-converting, responsive landing page for the **OMNIGO Super App** (Ride Bidding, Express Courier & Delivery, Multi-Vendor Marketplace, and Escrow Fintech).

---

## 🎨 Design System & Palette Used
This website uses the exact design tokens extracted from the **OMNIGO Flutter Application**:
- **Base Background:** `#0F0F0F` (Deep Void OLED Black)
- **Surfaces & Cards:** `#141414` (Matte Obsidian) & `#16161D`
- **Primary Accent:** `#CAFF33` (Electric Neon Lime)
- **Secondary Accents:** `#33FFA6` (Cyber Mint) & `#33D1FF` (Cyber Sky Blue)
- **Fintech Card:** Linear gradient from `#303F9F` (Indigo 700) to `#1A237E` (Indigo 900)
- **Glassmorphism:** `backdrop-filter: blur(20px)` with `1px solid rgba(255, 255, 255, 0.08)`

---

## 🚀 Features Included
1. **Sticky Glassmorphic Navigation Bar:** Floating pill design with quick links and instant download button.
2. **Hero Section with Interactive Fare Bidding Demo:** Live counter (`+` and `-` buttons) allowing visitors to test the InDrive-style fare negotiation mechanic.
3. **Ecosystem & Services Switcher:** Interactive tab switching for **Customers**, **Drivers/Riders**, and **Merchants/Vendors**.
4. **Fintech & Escrow Vault:** 3D digital card simulation highlighting 100% fraud protection, PayFast, and instant payouts.
5. **AI Self-Healing & Surveillance Engine:** High-tech command center showcase with real-time status diagnostics.
6. **Direct APK Download System:** 
   - All download buttons trigger direct downloading of `downloads/omnigo-app-release.apk` (118 MB).
   - Includes a custom toast notification when the download begins.
   - Includes a full Download Modal offering Direct APK, Google Play, and Apple App Store options.
7. **100% Responsive Design:** Tailored for Mobile, Tablet, and Desktop.

---

## 📁 File Structure
```
OMNIGO Website/
├── index.html        # Main landing page (HTML5 + Tailwind CSS + Lucide Icons)
├── styles.css        # Custom styles, animations & glassmorphism filters
├── app.js            # Interactive logic (bidding simulator, tabs, downloads)
├── downloads/        # Direct APK downloads folder
│   └── omnigo-app-release.apk  # Official release APK build (118 MB)
└── README.md         # Documentation
```

---

## 💻 How to Run Locally

### Option 1: Python HTTP Server (Zero Setup)
```bash
cd "OMNIGO Website"
python3 -m http.server 3000
```
Open your browser at: `http://localhost:3000`

### Option 2: Node.js (npx serve)
```bash
cd "OMNIGO Website"
npx serve .
```

### Option 3: Deploy to Vercel / Netlify / GitHub Pages
Simply push this folder to GitHub or drag-and-drop the `OMNIGO Website` folder into [Netlify Drop](https://app.netlify.com/drop) or [Vercel](https://vercel.com) for instant live hosting with custom domain support.
