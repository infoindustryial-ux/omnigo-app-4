// OMNIGO Super App Interactive Logic, Map Fleet Engine, Editorial Character Illustrations & Light Mode

document.addEventListener('DOMContentLoaded', () => {
  // ========================================================
  // 1. LIGHT / DARK MODE TOGGLE ENGINE
  // ========================================================
  const themeToggleBtn = document.getElementById('theme-toggle-btn');
  const themeToggleIcon = document.getElementById('theme-toggle-icon');
  const mobileThemeToggleBtn = document.getElementById('mobile-theme-toggle-btn');
  const mobileThemeText = document.getElementById('mobile-theme-text');

  function applyTheme(theme) {
    const isLight = theme === 'light';
    document.body.classList.toggle('light-mode', isLight);
    localStorage.setItem('omnigo_theme', theme);
    if (themeToggleIcon) themeToggleIcon.setAttribute('data-lucide', isLight ? 'moon' : 'sun');
    if (mobileThemeText) mobileThemeText.innerText = isLight ? 'Switch to Dark Mode' : 'Switch to Light Mode';
    if (window.lucide) lucide.createIcons();
  }

  const savedTheme = localStorage.getItem('omnigo_theme');
  if (savedTheme) {
    applyTheme(savedTheme);
  }

  function toggleTheme() {
    const isCurrentlyLight = document.body.classList.contains('light-mode');
    applyTheme(isCurrentlyLight ? 'dark' : 'light');
  }

  if (themeToggleBtn) themeToggleBtn.addEventListener('click', toggleTheme);
  if (mobileThemeToggleBtn) mobileThemeToggleBtn.addEventListener('click', toggleTheme);

  // ========================================================
  // 2. VEHICLE FLEET ENGINE & MAP ANIMATION
  // ========================================================
  const vehicles = {
    bike: {
      name: 'OMNIGO Moto',
      desc: 'Fastest in traffic • 1 Rider or Parcel',
      baseFare: 180,
      eta: '11 Mins',
      driver: 'Driver Bilal (Honda CG 125)',
      rating: '4.9 ★',
      speedClass: 'speed-fast',
      svg: `
        <!-- High-End Moto SVG -->
        <polygon points="10,-3 22,-6 22,6 10,3" fill="rgba(202, 255, 51, 0.45)" />
        <circle cx="0" cy="0" r="12" fill="#CAFF33" stroke="#000" stroke-width="2" />
        <circle cx="-5" cy="4" r="2.5" fill="#000" />
        <circle cx="5" cy="4" r="2.5" fill="#000" />
        <path d="M-4 -2 L0 -6 L4 -2" stroke="#000" stroke-width="1.8" fill="none" />
        <circle cx="0" cy="-4" r="2" fill="#000" />
      `
    },
    rickshaw: {
      name: 'Auto Rickshaw',
      desc: 'Budget city travel • Up to 3 passengers',
      baseFare: 290,
      eta: '18 Mins',
      driver: 'Driver Tariq (Sazgar 4-Stroke)',
      rating: '4.8 ★',
      speedClass: 'speed-slow',
      svg: `
        <!-- High-End Auto Rickshaw SVG -->
        <polygon points="12,-4 22,-7 22,7 12,4" fill="rgba(51, 255, 166, 0.4)" />
        <rect x="-9" y="-8" width="18" height="15" rx="3" fill="#33FFA6" stroke="#000" stroke-width="2" />
        <rect x="-6" y="-6" width="12" height="6" rx="1" fill="#141414" />
        <circle cx="-6" cy="6" r="2.5" fill="#000" />
        <circle cx="6" cy="6" r="2.5" fill="#000" />
        <circle cx="0" cy="7" r="2" fill="#000" />
      `
    },
    mini: {
      name: 'Economy Car',
      desc: 'Suzuki Alto / Cultus • AC Ride',
      baseFare: 450,
      eta: '15 Mins',
      driver: 'Driver Ahmad (Suzuki Alto 2023)',
      rating: '4.95 ★',
      speedClass: 'speed-medium',
      svg: `
        <!-- High-End Economy Car SVG -->
        <polygon points="12,-4 26,-8 26,8 12,4" fill="rgba(202, 255, 51, 0.35)" />
        <circle cx="0" cy="0" r="13" fill="#CAFF33" stroke="#000" stroke-width="2" />
        <path d="M-6 -3 L-3 -6 L3 -6 L6 -3 L6 4 L-6 4 Z" fill="#000000" />
        <circle cx="-3" cy="4" r="1.5" fill="#000000" />
        <circle cx="3" cy="4" r="1.5" fill="#000000" />
      `
    },
    ac: {
      name: 'Comfort Sedan',
      desc: 'Toyota Yaris / Honda City • Executive AC',
      baseFare: 650,
      eta: '13 Mins',
      driver: 'Captain Usman (Toyota Yaris 2022)',
      rating: '5.0 ★',
      speedClass: 'speed-medium',
      svg: `
        <!-- High-End AC Sedan SVG -->
        <polygon points="14,-5 30,-9 30,9 14,5" fill="rgba(51, 209, 255, 0.4)" />
        <rect x="-10" y="-7" width="22" height="14" rx="4" fill="#33D1FF" stroke="#000" stroke-width="2" />
        <rect x="-6" y="-5" width="14" height="6" rx="2" fill="#0F0F0F" />
        <circle cx="-6" cy="6" r="2.5" fill="#000" />
        <circle cx="6" cy="6" r="2.5" fill="#000" />
      `
    }
  };

  let selectedVehicleKey = 'mini';
  let currentFare = vehicles[selectedVehicleKey].baseFare;
  let tripState = 'idle';

  const fareDisplay = document.getElementById('bidding-fare-amount');
  const decreaseBtn = document.getElementById('bidding-decrease-btn');
  const increaseBtn = document.getElementById('bidding-increase-btn');
  const acceptOfferBtn = document.getElementById('bidding-accept-btn');
  const biddingStatusText = document.getElementById('bidding-status-text');
  const driverDetailBadge = document.getElementById('map-driver-badge');
  const etaBadge = document.getElementById('map-eta-badge');
  const vehicleTypePill = document.getElementById('map-vehicle-pill');
  const routeStatusPill = document.getElementById('route-status-pill');
  const movingVehicleNode = document.getElementById('moving-vehicle-node');
  const movingVehicleGraphic = document.getElementById('moving-vehicle-graphic');

  function updateVehicleVisuals() {
    const v = vehicles[selectedVehicleKey];
    currentFare = v.baseFare;

    if (fareDisplay) {
      fareDisplay.innerText = `PKR ${currentFare}`;
      fareDisplay.classList.add('scale-110');
      setTimeout(() => fareDisplay.classList.remove('scale-110'), 200);
    }

    if (movingVehicleGraphic && movingVehicleNode) {
      movingVehicleGraphic.innerHTML = v.svg;
      movingVehicleNode.className = `moving-car-node ${v.speedClass}`;
    }

    if (acceptOfferBtn && tripState === 'idle') {
      acceptOfferBtn.innerHTML = `
        <i data-lucide="check-circle" class="w-4 h-4"></i>
        <span>Offer PKR ${currentFare} for ${v.name}</span>
      `;
      if (window.lucide) lucide.createIcons();
    }

    if (driverDetailBadge) {
      driverDetailBadge.innerHTML = `<strong>${v.driver}</strong> • ${v.rating}`;
    }

    if (etaBadge) etaBadge.innerText = v.eta;
    if (vehicleTypePill) vehicleTypePill.innerText = v.name;
    if (biddingStatusText && tripState === 'idle') {
      biddingStatusText.innerText = `Nearby drivers bidding for this ${v.name}...`;
    }
  }

  const vehicleCards = document.querySelectorAll('[data-vehicle-select]');
  vehicleCards.forEach(card => {
    card.addEventListener('click', () => {
      const type = card.getAttribute('data-vehicle-select');
      if (vehicles[type]) {
        selectedVehicleKey = type;
        vehicleCards.forEach(c => {
          c.classList.remove('border-omnigo-lime', 'bg-omnigo-lime/10', 'scale-[1.02]');
          c.classList.add('border-white/10', 'bg-white/5');
        });
        card.classList.add('border-omnigo-lime', 'bg-omnigo-lime/10', 'scale-[1.02]');
        card.classList.remove('border-white/10', 'bg-white/5');
        updateVehicleVisuals();
      }
    });
  });

  function adjustFare(amount) {
    const minFare = Math.max(100, vehicles[selectedVehicleKey].baseFare - 150);
    const newFare = currentFare + amount;
    if (newFare >= minFare && newFare <= 5000) {
      currentFare = newFare;
      if (fareDisplay) {
        fareDisplay.innerText = `PKR ${currentFare}`;
        fareDisplay.classList.add('scale-110');
        setTimeout(() => fareDisplay.classList.remove('scale-110'), 180);
      }
      if (acceptOfferBtn && tripState === 'idle') {
        acceptOfferBtn.innerHTML = `
          <i data-lucide="check-circle" class="w-4 h-4"></i>
          <span>Offer PKR ${currentFare} to Drivers</span>
        `;
        if (window.lucide) lucide.createIcons();
      }
    }
  }

  if (decreaseBtn) decreaseBtn.addEventListener('click', () => adjustFare(-50));
  if (increaseBtn) increaseBtn.addEventListener('click', () => adjustFare(50));

  if (acceptOfferBtn) {
    acceptOfferBtn.addEventListener('click', () => {
      if (tripState === 'idle') {
        tripState = 'matching';
        acceptOfferBtn.disabled = true;
        acceptOfferBtn.innerHTML = `
          <span class="inline-flex items-center gap-2">
            <svg class="animate-spin h-5 w-5 text-black" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
            </svg>
            Broadcasting Offer to Drivers...
          </span>
        `;
        if (biddingStatusText) {
          biddingStatusText.innerText = "Drivers reviewing your offer of PKR " + currentFare + "...";
        }

        setTimeout(() => {
          tripState = 'en_route';
          const v = vehicles[selectedVehicleKey];
          acceptOfferBtn.innerHTML = `✓ Driver Confirmed! • OTP: 8492`;
          acceptOfferBtn.classList.remove('bg-omnigo-lime');
          acceptOfferBtn.classList.add('bg-omnigo-mint');

          if (biddingStatusText) {
            biddingStatusText.innerHTML = `<span class="text-omnigo-mint font-bold">Trip Confirmed!</span> ${v.driver} is arriving in 3 mins.`;
          }

          if (routeStatusPill) {
            routeStatusPill.innerText = "● En Route (Live GPS)";
            routeStatusPill.className = "text-[11px] px-2.5 py-0.5 rounded-full bg-omnigo-mint/20 text-omnigo-mint border border-omnigo-mint/40 font-semibold";
          }

          setTimeout(() => {
            tripState = 'idle';
            acceptOfferBtn.disabled = false;
            acceptOfferBtn.classList.remove('bg-omnigo-mint');
            acceptOfferBtn.classList.add('bg-omnigo-lime');
            updateVehicleVisuals();
            if (routeStatusPill) {
              routeStatusPill.innerText = "● 4 Drivers Nearby";
              routeStatusPill.className = "text-[11px] px-2.5 py-0.5 rounded-full bg-omnigo-lime/15 text-omnigo-lime border border-omnigo-lime/30 font-semibold";
            }
          }, 6000);

        }, 1800);
      }
    });
  }

  // ========================================================
  // 3. EDITORIAL ILLUSTRATED HUMAN STORY PIPELINE
  // ========================================================
  const storySteps = [
    {
      actor: 'customer',
      time: '12:30 PM',
      title: '1. Customer Places Order & Funds Locked in Escrow',
      urdu: 'کسٹمر اپنے موبائل سے آرڈر کرتا ہے اور رقم اسکرو والٹ میں لاک ہوتی ہے',
      messageText: 'Ayesha placed Order #OMN-8429 (Wireless Headphones). PKR 2,850 securely locked in Escrow Vault.',
      messageIcon: 'shopping-cart',
      messageColor: 'border-omnigo-lime text-omnigo-lime',
      custBubble: 'Ordered! 🔒 Money locked safely in Escrow.',
      vendorBubble: 'Waiting for orders...',
      riderBubble: 'Online in Gulberg area.',
      custPhoneScreen: `
        <div class="p-2 text-center">
          <div class="text-[9px] text-gray-400">OMNIGO Checkout</div>
          <div class="text-[12px] font-black text-omnigo-lime mt-0.5">PKR 2,850</div>
          <div class="text-[8px] bg-emerald-500/20 text-emerald-400 rounded py-0.5 mt-1 font-bold">🔒 Escrow Active</div>
          <div class="text-[8px] text-gray-300 mt-1">Waiting Vendor Prep</div>
        </div>
      `,
      vendorPhoneScreen: `
        <div class="p-2 text-center text-gray-400 text-[9px]">
          Store Open • Online
        </div>
      `,
      riderPhoneScreen: `
        <div class="p-2 text-center text-gray-400 text-[9px] flex items-center justify-center gap-1.5">
          <span class="w-1.5 h-1.5 rounded-full bg-omnigo-mint animate-ping"></span>
          <span>Online in Gulberg • Ready</span>
        </div>
      `,
      centerStageGraphic: `
        <div class="flex flex-col items-center justify-center p-4 text-center">
          <div class="w-14 h-14 rounded-2xl bg-omnigo-lime/15 text-omnigo-lime border border-omnigo-lime/30 flex items-center justify-center mb-2 animate-pulse">
            <i data-lucide="lock" class="w-7 h-7"></i>
          </div>
          <div class="font-bold text-sm text-white">Escrow Cryptographic Lock</div>
          <div class="text-xs text-gray-400 mt-1">PKR 2,850 securely frozen. Vendor cannot steal, buyer is protected.</div>
        </div>
      `
    },
    {
      actor: 'vendor',
      time: '12:32 PM',
      title: '2. Merchant Receives Chime & Packs Parcel with Tamper Seal',
      urdu: 'وینڈر کو دکان کے ٹیبلٹ پر الرٹ ملا اور اس نے سامان محفوظ پیک کیا',
      messageText: 'Gulberg Tech Store: Order #OMN-8429 accepted & packed with security seal tape.',
      messageIcon: 'store',
      messageColor: 'border-omnigo-cyan text-omnigo-cyan',
      custBubble: 'Store is packing my item.',
      vendorBubble: 'Ding! Order Received. Packed & Sealed! 📦',
      riderBubble: 'Broadcasting nearby...',
      custPhoneScreen: `
        <div class="p-2 text-center">
          <div class="text-[9px] text-gray-400">Order #OMN-8429</div>
          <div class="text-[10px] font-bold text-omnigo-cyan mt-1">📦 Being Packed</div>
          <div class="text-[8px] text-gray-400 mt-1">Gulberg Tech Mart</div>
        </div>
      `,
      vendorPhoneScreen: `
        <div class="p-2 text-center">
          <div class="text-[9px] text-omnigo-cyan font-bold">NEW ORDER #8429</div>
          <div class="text-[11px] font-black text-white">Headphones x 1</div>
          <button class="text-[8px] bg-omnigo-cyan text-black font-extrabold px-2 py-0.5 rounded mt-1">
            ✓ Pack Complete
          </button>
        </div>
      `,
      riderPhoneScreen: `
        <div class="p-2 text-center">
          <div class="text-[9px] text-omnigo-lime font-bold">● DISPATCH BROADCAST</div>
          <div class="text-[11px] font-black text-white">Gulberg Tech Mart</div>
          <div class="text-[8px] text-gray-400">Parcel Pickup • Fee PKR 250</div>
        </div>
      `,
      centerStageGraphic: `
        <div class="flex flex-col items-center justify-center p-4 text-center">
          <div class="w-14 h-14 rounded-2xl bg-omnigo-cyan/15 text-omnigo-cyan border border-omnigo-cyan/30 flex items-center justify-center mb-2 animate-bounce">
            <i data-lucide="package-check" class="w-7 h-7"></i>
          </div>
          <div class="font-bold text-sm text-white">Merchant Packaging Complete</div>
          <div class="text-xs text-gray-400 mt-1">Automated AI fleet dispatcher matching nearest rider...</div>
        </div>
      `
    },
    {
      actor: 'rider',
      time: '12:35 PM',
      title: '3. Anti-Fraud Inspection & Pickup Photo Proof by Rider',
      urdu: 'فراڈ سے بچاؤ: رائیڈر دکان پر سامان کی کیمرے سے تصویر کھینچ کر تصدیق کرتا ہے',
      messageText: 'Rider Farhan arrived at store. 📸 Photo proof uploaded (pickup_photo_url). Item verified authentic.',
      messageIcon: 'camera',
      messageColor: 'border-amber-400 text-amber-300',
      custBubble: 'Rider is verifying parcel quality.',
      vendorBubble: 'Rider verified my package with photo!',
      riderBubble: '📸 Snapped inspection photo! Sealed & Clean.',
      custPhoneScreen: `
        <div class="p-2 text-center">
          <div class="text-[9px] text-gray-400">Photo Verified</div>
          <div class="text-[10px] font-bold text-amber-300 mt-1">📸 In-App Audit</div>
          <div class="text-[8px] text-gray-300 mt-1">Rider Farhan Verified</div>
        </div>
      `,
      vendorPhoneScreen: `
        <div class="p-2 text-center">
          <div class="text-[9px] text-amber-300 font-bold">HANDOVER COMPLETE</div>
          <div class="text-[8px] text-gray-300 mt-1">Photo Uploaded</div>
        </div>
      `,
      riderPhoneScreen: `
        <div class="p-2 text-center">
          <div class="text-[9px] text-amber-300 font-bold">📸 CAMERA AUDIT COMPLETE</div>
          <div class="text-[10px] text-white font-mono">pickup_photo_url: OK</div>
          <div class="text-[8px] text-emerald-400 font-bold">✓ Tamper Seal Intact</div>
        </div>
      `,
      centerStageGraphic: `
        <div class="flex flex-col items-center justify-center p-4 text-center relative">
          <div id="camera-flash-box" class="w-16 h-16 rounded-2xl bg-amber-400/20 text-amber-300 border border-amber-400/40 flex items-center justify-center mb-2 animate-pulse">
            <i data-lucide="camera" class="w-8 h-8"></i>
          </div>
          <div class="font-bold text-sm text-white">Anti-Fraud Photo Proof Captured</div>
          <div class="text-xs text-amber-300 font-mono mt-1">pickup_photo_url recorded in ledger</div>
          <div class="text-[11px] text-gray-400 mt-1">Zero fake boxes or missing accessories!</div>
        </div>
      `
    },
    {
      actor: 'rider',
      time: '12:41 PM',
      title: '4. Live GPS Telemetry Transit to Customer Destination',
      urdu: 'لائیو جی پی ایس ٹریکنگ: رائیڈر تیز رفتاری سے کسٹمر کے گھر کی طرف رواں دواں',
      messageText: 'Rider Farhan en-route to DHA Phase 5. Live GPS telemetry streaming (ETA: 4 Mins).',
      messageIcon: 'navigation',
      messageColor: 'border-omnigo-mint text-omnigo-mint',
      custBubble: 'I can see rider moving on map! 🛵',
      vendorBubble: 'Order in transit.',
      riderBubble: 'Heading to customer destination. ETA 4m.',
      custPhoneScreen: `
        <div class="p-2 text-center">
          <div class="text-[9px] text-omnigo-mint font-bold">● LIVE TRACKING</div>
          <div class="text-[10px] text-white font-bold mt-0.5">ETA: 4 Mins</div>
          <div class="text-[8px] text-gray-400 mt-0.5">Rider: Farhan</div>
        </div>
      `,
      vendorPhoneScreen: `
        <div class="p-2 text-center text-[9px] text-gray-400">
          In Transit • Live Map
        </div>
      `,
      riderPhoneScreen: `
        <div class="p-2 text-center">
          <div class="text-[9px] text-omnigo-mint font-bold">● LIVE TELEMETRY STREAM</div>
          <div class="text-[11px] text-white font-black">Speed: 42 km/h • ETA: 4m</div>
          <div class="text-[8px] text-gray-400">Route Deviation Guard: Active</div>
        </div>
      `,
      centerStageGraphic: `
        <div class="flex flex-col items-center justify-center p-4 text-center">
          <div class="w-14 h-14 rounded-2xl bg-omnigo-mint/15 text-omnigo-mint border border-omnigo-mint/30 flex items-center justify-center mb-2 animate-float">
            <i data-lucide="navigation-2" class="w-7 h-7"></i>
          </div>
          <div class="font-bold text-sm text-white">Real-Time WebSocket GPS Stream</div>
          <div class="text-xs text-gray-400 mt-1">Sub-second location pings. Route deviation security guard active.</div>
        </div>
      `
    },
    {
      actor: 'customer',
      time: '12:45 PM',
      title: '5. Doorstep Delivery Photo + 4-Digit OTP & Escrow Settlement',
      urdu: 'ڈلیوری مکمل: کسٹمر نے 4 ہندسوں کا OTP دیا، اور دکاندار کو پیسے جاری ہو گئے',
      messageText: 'Delivery confirmed! Doorstep photo taken & OTP (8492) verified. PKR 2,850 released to Merchant & Rider!',
      messageIcon: 'check-check',
      messageColor: 'border-emerald-400 text-emerald-400',
      custBubble: 'Received authentic item! Given OTP: 8492. ⭐⭐⭐⭐⭐',
      vendorBubble: 'Cha-ching! PKR 2,850 credited to my wallet! 💰',
      riderBubble: 'Trip completed! Fare credited to wallet.',
      custPhoneScreen: `
        <div class="p-2 text-center">
          <div class="text-[9px] text-emerald-400 font-bold">✓ DELIVERED</div>
          <div class="text-[10px] text-white font-bold mt-1">OTP: 8492 Verified</div>
          <div class="text-[8px] text-gray-300 mt-1">5★ Rating Given</div>
        </div>
      `,
      vendorPhoneScreen: `
        <div class="p-2 text-center">
          <div class="text-[9px] text-emerald-400 font-bold">PAID: PKR 2,850</div>
          <div class="text-[8px] text-gray-300 mt-1">Transferred to Wallet</div>
        </div>
      `,
      riderPhoneScreen: `
        <div class="p-2 text-center">
          <div class="text-[9px] text-emerald-400 font-bold">✓ TRIP SETTLED</div>
          <div class="text-[11px] text-white font-black">PKR 250 in Wallet</div>
          <div class="text-[8px] text-emerald-400">OTP Verified • 5★ Trip</div>
        </div>
      `,
      centerStageGraphic: `
        <div class="flex flex-col items-center justify-center p-4 text-center">
          <div class="w-14 h-14 rounded-2xl bg-emerald-500/20 text-emerald-400 border border-emerald-500/40 flex items-center justify-center mb-2 animate-pulse">
            <i data-lucide="shield-check" class="w-8 h-8"></i>
          </div>
          <div class="font-bold text-sm text-emerald-400">Escrow Successfully Released!</div>
          <div class="text-xs text-gray-300 mt-1">Customer verified box &bull; Merchant got paid &bull; Zero fraud!</div>
        </div>
      `
    }
  ];

  let currentStoryIdx = 0;
  let storyAutoTimer = null;

  const storyTopNotification = document.getElementById('story-top-notification');
  const storyNotifIcon = document.getElementById('story-notif-icon');
  const storyNotifTime = document.getElementById('story-notif-time');
  const storyNotifText = document.getElementById('story-notif-text');

  const storyHeading = document.getElementById('story-heading');
  const storyUrdu = document.getElementById('story-urdu');
  
  const customerCard = document.getElementById('story-customer-card');
  const vendorCard = document.getElementById('story-vendor-card');
  const riderCard = document.getElementById('story-rider-card');

  const customerBubble = document.getElementById('story-customer-bubble');
  const vendorBubble = document.getElementById('story-vendor-bubble');
  const riderBubble = document.getElementById('story-rider-bubble');

  const customerPhone = document.getElementById('story-customer-phone');
  const riderPhone = document.getElementById('story-rider-phone');
  const vendorPhone = document.getElementById('story-vendor-phone');
  const centerStage = document.getElementById('story-center-stage');

  const storyStepButtons = document.querySelectorAll('[data-story-step]');
  const playStoryBtn = document.getElementById('play-story-btn');

  function renderStoryStep(idx) {
    currentStoryIdx = idx;
    const step = storySteps[idx];

    if (storyTopNotification) {
      storyTopNotification.className = `notification-slide-in rounded-2xl p-4 bg-black/80 border ${step.messageColor} shadow-2xl flex items-center justify-between gap-4 transition-all`;
      if (storyNotifTime) storyNotifTime.innerText = step.time;
      if (storyNotifText) storyNotifText.innerText = step.messageText;
      if (storyNotifIcon) {
        storyNotifIcon.setAttribute('data-lucide', step.messageIcon);
      }
    }

    if (storyHeading) storyHeading.innerText = step.title;
    if (storyUrdu) storyUrdu.innerText = step.urdu;

    if (customerBubble) customerBubble.innerText = step.custBubble;
    if (vendorBubble) vendorBubble.innerText = step.vendorBubble;
    if (riderBubble) riderBubble.innerText = step.riderBubble;

    if (customerPhone) customerPhone.innerHTML = step.custPhoneScreen;
    if (riderPhone && step.riderPhoneScreen) riderPhone.innerHTML = step.riderPhoneScreen;
    if (vendorPhone) vendorPhone.innerHTML = step.vendorPhoneScreen;
    if (centerStage) centerStage.innerHTML = step.centerStageGraphic;

    if (customerCard) customerCard.classList.remove('active-actor-glow');
    if (vendorCard) vendorCard.classList.remove('active-actor-glow');
    if (riderCard) riderCard.classList.remove('active-actor-glow');

    if (step.actor === 'customer' && customerCard) customerCard.classList.add('active-actor-glow');
    if (step.actor === 'vendor' && vendorCard) vendorCard.classList.add('active-actor-glow');
    if (step.actor === 'rider' && riderCard) riderCard.classList.add('active-actor-glow');

    storyStepButtons.forEach((btn, i) => {
      if (i === idx) {
        btn.className = "px-3.5 py-1.5 rounded-full text-xs font-bold bg-omnigo-lime text-black shadow-lg scale-105 transition-all";
      } else {
        btn.className = "px-3.5 py-1.5 rounded-full text-xs font-medium text-gray-400 bg-white/5 hover:bg-white/10 transition-all";
      }
    });

    if (window.lucide) lucide.createIcons();
  }

  storyStepButtons.forEach(btn => {
    btn.addEventListener('click', () => {
      const idx = parseInt(btn.getAttribute('data-story-step'));
      if (!isNaN(idx)) renderStoryStep(idx);
    });
  });

  if (playStoryBtn) {
    playStoryBtn.addEventListener('click', () => {
      if (storyAutoTimer) {
        clearInterval(storyAutoTimer);
        storyAutoTimer = null;
        playStoryBtn.innerHTML = `
          <i data-lucide="play" class="w-3.5 h-3.5"></i>
          <span>Play Story Flow</span>
        `;
        if (window.lucide) lucide.createIcons();
        return;
      }

      playStoryBtn.innerHTML = `
        <i data-lucide="pause" class="w-3.5 h-3.5"></i>
        <span>Pause Story</span>
      `;
      if (window.lucide) lucide.createIcons();

      storyAutoTimer = setInterval(() => {
        const next = (currentStoryIdx + 1) % storySteps.length;
        renderStoryStep(next);
      }, 4000);
    });
  }

  // ========================================================
  // 4. ECOSYSTEM TABS
  // ========================================================
  const tabButtons = document.querySelectorAll('[data-tab-target]');
  const tabContents = document.querySelectorAll('[data-tab-content]');

  tabButtons.forEach(btn => {
    btn.addEventListener('click', () => {
      const target = btn.getAttribute('data-tab-target');

      tabButtons.forEach(b => {
        b.classList.remove('bg-omnigo-lime', 'text-black', 'shadow-lg');
        b.classList.add('text-white/70', 'bg-white/5');
      });
      btn.classList.add('bg-omnigo-lime', 'text-black', 'shadow-lg');
      btn.classList.remove('text-white/70', 'bg-white/5');

      tabContents.forEach(content => {
        if (content.getAttribute('data-tab-content') === target) {
          content.classList.remove('hidden');
          content.classList.add('grid');
        } else {
          content.classList.add('hidden');
          content.classList.remove('grid');
        }
      });
    });
  });

  // ========================================================
  // 5. DIRECT APK DOWNLOAD
  // ========================================================
  const downloadTriggers = document.querySelectorAll('.trigger-apk-download');
  const toast = document.getElementById('download-toast');

  downloadTriggers.forEach(btn => {
    btn.addEventListener('click', () => {
      const apkLink = document.createElement('a');
      apkLink.href = 'downloads/omnigo-app-release.apk';
      apkLink.download = 'OMNIGO-SuperApp-v1.0.apk';
      document.body.appendChild(apkLink);
      apkLink.click();
      document.body.removeChild(apkLink);

      if (toast) {
        toast.classList.remove('translate-y-20', 'opacity-0');
        toast.classList.add('translate-y-0', 'opacity-100');
        setTimeout(() => {
          toast.classList.add('translate-y-20', 'opacity-0');
          toast.classList.remove('translate-y-0', 'opacity-100');
        }, 5000);
      }
    });
  });

  const modal = document.getElementById('download-modal');
  const openModalBtns = document.querySelectorAll('.open-download-modal');
  const closeModalBtns = document.querySelectorAll('.close-download-modal');

  openModalBtns.forEach(btn => {
    btn.addEventListener('click', (e) => {
      e.preventDefault();
      if (modal) modal.classList.remove('hidden');
    });
  });

  closeModalBtns.forEach(btn => {
    btn.addEventListener('click', () => {
      if (modal) modal.classList.add('hidden');
    });
  });

  if (modal) {
    modal.addEventListener('click', (e) => {
      if (e.target === modal) modal.classList.add('hidden');
    });
  }

  // Mobile Menu Toggles
  const mobileMenuBtn = document.getElementById('mobile-menu-btn');
  const mobileMenuDrawer = document.getElementById('mobile-menu-drawer');
  const closeMobileMenu = document.getElementById('close-mobile-menu');

  if (mobileMenuBtn && mobileMenuDrawer) {
    mobileMenuBtn.addEventListener('click', () => {
      mobileMenuDrawer.classList.toggle('hidden');
    });
  }

  if (closeMobileMenu && mobileMenuDrawer) {
    closeMobileMenu.addEventListener('click', () => {
      mobileMenuDrawer.classList.add('hidden');
    });
  }

  document.querySelectorAll('.mobile-nav-link').forEach(link => {
    link.addEventListener('click', () => {
      if (mobileMenuDrawer) mobileMenuDrawer.classList.add('hidden');
    });
  });

  // Initial renders
  updateVehicleVisuals();
  renderStoryStep(0);
});
