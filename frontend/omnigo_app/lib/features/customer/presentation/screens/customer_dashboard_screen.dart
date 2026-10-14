import 'dart:async';
import 'dart:convert';
import 'dart:ui';
import 'package:flutter/material.dart';

import 'package:maplibre_gl/maplibre_gl.dart';

import 'package:geolocator/geolocator.dart';
import 'package:url_launcher/url_launcher.dart';
import '../../../../core/theme/app_theme.dart';
import '../../../../core/services/session_registry.dart';
import '../../../../core/network/api_endpoints.dart';
import '../../../../core/network/api_client.dart';
import '../../../../core/network/websocket_client.dart';
import '../../../../core/di/service_locator.dart';
import 'package:provider/provider.dart';
import '../../../../core/services/cart_provider.dart';
import '../../data/models/product.dart';
import 'product_details_screen.dart';
import 'edit_profile_screen.dart';
import 'wishlist_screen.dart';
import 'cart_screen.dart';
import '../widgets/vehicle_selector_sheet.dart';
import 'customer_wallet_screen.dart';
import 'customer_saved_cards_screen.dart';
import 'security_settings_screen.dart';
import 'my_orders_screen.dart';
import '../../../shared/presentation/widgets/map_libre_map_widget.dart';
import '../../../../shared/presentation/widgets/chat_nav_button.dart';
import '../../../../shared/presentation/services/chat_service.dart';

class CustomerDashboardScreen extends StatefulWidget {
  const CustomerDashboardScreen({super.key, required this.trackingId});
  final String trackingId;

  @override
  CustomerDashboardScreenState createState() =>
      CustomerDashboardScreenState();
}

class CustomerDashboardScreenState extends State<CustomerDashboardScreen> {
  int _currentIndex = 0;
  final GlobalKey<WishlistScreenState> _wishlistKey = GlobalKey<WishlistScreenState>();
  MapLibreMapController? _mapController;
  // Default to Pakistan's centroid (≈ Rahim Yar Khan) — neutral, not
  // Lahore-specific. This is only shown for the few seconds before the
  // first GPS fix arrives, after which _fetchMapCenterFromGPS() replaces
  // it. We deliberately do NOT bias the map to a specific city.
  LatLng _mapCenter = const LatLng(30.3753, 69.3451);
  final Map<String, MarkerData> _mapMarkers = {};
  final TextEditingController _mapSearchController = TextEditingController();
  final TextEditingController _searchController = TextEditingController();

  String _searchQuery = '';
  String _selectedCategory = 'All';
  Timer? _searchDebounce;

  // Strict UI Loading state for Button Debouncing


  // Pagination & API state
  bool _isLoadingCatalog = false;
  bool _hasMore = true;
  int _offset = 0;
  final int _limit = 20;
  List<Product> _allProducts = [];
  final ScrollController _scrollController = ScrollController();

  List<dynamic> _customerOrders = [];
  bool _isLoadingOrders = false;

  // ── Wishlist state ───────────────────────────────────────────────
  Set<String> _favoriteProductIds = {};
  bool _isLoadingWishlist = false;

  bool _isGeocoding = false;

  // ── Live Rider Tracking (WebSocket telemetry) ────────────────────
  WebSocketClient? _wsClient;
  StreamSubscription<dynamic>? _wsSub;
  StreamSubscription<dynamic>? _wsOrderTopicSub;
  StreamSubscription<WSConnectionState>? _wsStateSub;
  // riderTrackingId → latest LatLng, broadcast so Map tab rebuilds only
  // the marker layer without touching the rest of the dashboard.
  final ValueNotifier<Map<String, LatLng>> _riderMarkers =
      ValueNotifier<Map<String, LatLng>>({});

  // ── Ride Hailing / Dynamic Pricing State ─────────────────────────
  List<LatLng> _rideRoutePolyline = [];
  LatLng? _ridePickupLatLng;
  LatLng? _rideDropoffLatLng;
  bool _isEstimatingRide = false;
  Timer? _estimateSafetyTimer;

  // ── Ride Radar & Driver Discovery State ─────────────────────────
  String? _activeRideTrackingId;
  String _rideSearchStatus = 'idle'; // 'idle' | 'searching' | 'offers_received' | 'accepted'
  double _activeRideFare = 0.0;
  String _activeRideVehicleType = 'car';
  String _activeRidePaymentMethod = 'cash'; // 'cash' | 'wallet'
  int _nearbyRidersCount = 0;
  double _searchRadiusKm = 3.0;
  Timer? _radarSearchTimer;
  int _searchElapsedSeconds = 0;
  List<Map<String, dynamic>> _liveBids = [];
  Map<String, dynamic>? _acceptedRiderInfo;
  StreamSubscription<dynamic>? _wsRideEventsSub;

  // ── Delivery Route Tracking (Active Order on Map) ──────────────────
  List<LatLng> _deliveryRoutePolyline = [];
  double? _deliveryEtaSeconds;
  double? _deliveryDistanceMeters;
  String? _activeDeliveryOrderId;

  @override
  void initState() {
    super.initState();
    _fetchCatalog();
    _fetchOrders();
    _fetchWishlist();
    _fetchMapCenterFromGPS(); // Center map on real user location

    _scrollController.addListener(() {
      if (_scrollController.position.pixels >=
              _scrollController.position.maxScrollExtent - 200 &&
          !_isLoadingCatalog &&
          _hasMore) {
        _fetchCatalog();
      }
    });

    _mapMarkers['me'] = MarkerData(
      position: _mapCenter,
      iconSize: 1.0,
    );
  }

  /// Fetch real GPS location and center map on user's position.
  ///
  /// Production flow (Session 59):
  ///   1. Check service enabled → if off, show "Open Settings" dialog.
  ///   2. Check permission → walk through denied / deniedForever /
  ///      unableToDetermine with proper dialogs.
  ///   3. On success, replace the default centroid with the real fix and
  ///      pan the map to it.
  Future<void> _fetchMapCenterFromGPS() async {
    try {
      final bool serviceEnabled = await Geolocator.isLocationServiceEnabled();
      if (!serviceEnabled) {
        unawaited(_showLocationServiceOffDialog());
        return;
      }

      LocationPermission permission = await Geolocator.checkPermission();
      if (permission == LocationPermission.denied) {
        permission = await Geolocator.requestPermission();
        if (permission == LocationPermission.denied) {
          unawaited(_showPermissionDeniedDialog());
          return;
        }
      }
      if (permission == LocationPermission.deniedForever) {
        unawaited(_showPermissionPermanentlyDeniedDialog());
        return;
      }
      if (permission == LocationPermission.unableToDetermine) {
        permission = await Geolocator.requestPermission();
        if (permission != LocationPermission.always &&
            permission != LocationPermission.whileInUse) {
          unawaited(_showPermissionDeniedDialog());
          return;
        }
      }

      final position = await Geolocator.getCurrentPosition(
        locationSettings: const LocationSettings(
          accuracy: LocationAccuracy.high,
          timeLimit: Duration(seconds: 10),
        ),
      );

      if (mounted) {
        final realLocation = LatLng(position.latitude, position.longitude);
        setState(() {
          _mapCenter = realLocation;
          // Replace the default centroid marker with real location
          _mapMarkers.clear();
          _mapMarkers['me'] = MarkerData(
            position: realLocation,
            iconSize: 1.0,
          );
        });
        // Pan the map to the real fix.
        _mapController?.animateCamera(
          CameraUpdate.newLatLng(realLocation),
        );
      }
    } catch (e) {
      debugPrint('GPS fetch failed: $e');
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('Could not get your location. Search by address instead.'),
            duration: Duration(seconds: 3),
          ),
        );
      }
    }
  }

  Future<void> _showLocationServiceOffDialog() async {
    if (!mounted) return;
    return showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => AlertDialog(
        title: const Text('Location Service is Off'),
        content: const Text(
          'OMNIGO needs your location to show nearby stores and accurate '
          'delivery estimates. Please turn on Location in your device settings.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(ctx).pop(), child: const Text('Not now')),
          FilledButton(
            onPressed: () {
              Navigator.of(ctx).pop();
              Geolocator.openLocationSettings();
            },
            child: const Text('Open Settings'),
          ),
        ],
      ),
    );
  }

  Future<void> _showPermissionDeniedDialog() async {
    if (!mounted) return;
    return showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => AlertDialog(
        title: const Text('Location Permission Denied'),
        content: const Text(
          'OMNIGO cannot show your location without permission. '
          'Tap "Try Again" to request it again.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(ctx).pop(), child: const Text('Cancel')),
          FilledButton(
            onPressed: () {
              Navigator.of(ctx).pop();
              _fetchMapCenterFromGPS();
            },
            child: const Text('Try Again'),
          ),
        ],
      ),
    );
  }

  Future<void> _showPermissionPermanentlyDeniedDialog() async {
    if (!mounted) return;
    return showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (ctx) => AlertDialog(
        title: const Text('Permission Blocked'),
        content: const Text(
          'You have permanently denied Location permission. Open the system '
          'app settings to grant it.',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.of(ctx).pop(), child: const Text('Cancel')),
          FilledButton(
            onPressed: () {
              Navigator.of(ctx).pop();
              Geolocator.openAppSettings();
            },
            child: const Text('Open App Settings'),
          ),
        ],
      ),
    );
  }

  @override
  void dispose() {
    _searchDebounce?.cancel();
    _estimateSafetyTimer?.cancel();
    _radarSearchTimer?.cancel();
    _wsRideEventsSub?.cancel();
    _wsSub?.cancel();
    _wsOrderTopicSub?.cancel();
    _wsStateSub?.cancel();
    // #29: Do NOT disconnect singleton WS client — it's shared across the app.
    // Only cancel subscriptions; the singleton lifecycle is managed elsewhere.
    _riderMarkers.dispose();
    _scrollController.dispose();
    _mapSearchController.dispose();
    _searchController.dispose();
    _mapController?.dispose();
    super.dispose();
  }

  // ── Wishlist API calls ───────────────────────────────────────────

  Future<void> _fetchWishlist() async {
    if (_isLoadingWishlist) return;
    setState(() => _isLoadingWishlist = true);

    try {
      final api = sl<ApiClient>();
      final response = await api.get(ApiEndpoints.wishlistList());

      if (response is Map<String, dynamic>) {
        final List<dynamic> ids = (response['product_tracking_ids'] as List<dynamic>?) ?? <dynamic>[];
        if (mounted) {
          setState(() {
            _favoriteProductIds = ids.map((e) => e.toString()).toSet();
            _isLoadingWishlist = false;
          });
        }
      } else {
        if (mounted) setState(() => _isLoadingWishlist = false);
      }
    } catch (e) {
      if (mounted) setState(() => _isLoadingWishlist = false);
      debugPrint('Error fetching wishlist: $e');
    }
  }

  Future<void> _toggleFavorite(String productId) async {
    final wasFavorited = _favoriteProductIds.contains(productId);
    // Optimistic update
    setState(() {
      if (wasFavorited) {
        _favoriteProductIds.remove(productId);
      } else {
        _favoriteProductIds.add(productId);
      }
    });

    try {
      final api = sl<ApiClient>();
      final response = await api.post(ApiEndpoints.wishlistToggle(productId), {});

      // H1 FIX: Backend returns {"status": "success", "is_favorited": true}
      // Previously checked response['statusCode'] which backend never sent,
      // so rollback on API error never triggered.
      if (response is Map && response['status'] != 'success') {
        // Rollback on failure
        if (!mounted) return;
        setState(() {
          if (wasFavorited) {
            _favoriteProductIds.add(productId);
          } else {
            _favoriteProductIds.remove(productId);
          }
        });
      }
    } catch (e) {
      // Rollback on network error
      if (!mounted) return;
      setState(() {
        if (wasFavorited) {
          _favoriteProductIds.add(productId);
        } else {
          _favoriteProductIds.remove(productId);
        }
      });
    }
  }

  /// Connects to the Rust WebSocket gateway (port 8087) to receive live
  /// rider telemetry for any of this customer's shipped orders. Safe to
  /// call multiple times — guards against duplicate connections.
  void _connectRiderTelemetry() {
    if (_wsClient != null) return; // already connected
    final token = SessionRegistry.instance.token ?? '';
    if (token.isEmpty) return;

    _wsClient = sl<WebSocketClient>();
    _wsClient!.connect(
      token,
      clientType: 'customer',
      trackingId: SessionRegistry.instance.trackingId ?? '',
    );

    // Bind the chat service to the WS so incoming chat messages are
    // surfaced to the chat button's badge.
    ChatService.instance.bindToWebSocket(_wsClient!);
    ChatService.instance.setUserId(SessionRegistry.instance.trackingId ?? '');

    _wsSub = _wsClient!.topicStream('telemetry').listen((raw) {
      if (raw is! String) return;
      try {
        final frame = jsonDecode(raw) as Map<String, dynamic>;

        // Handle rider telemetry for live map
        final riderId = frame['rider_id']?.toString() ??
            frame['rider_tracking_id']?.toString() ??
            '';
        final lat = (frame['lat'] as num?)?.toDouble();
        final lng = (frame['lng'] as num?)?.toDouble();
        if (riderId.isEmpty || lat == null || lng == null) return;

        // Mutate a copy so ValueNotifier fires reliably.
        final updated = Map<String, LatLng>.from(_riderMarkers.value);
        updated[riderId] = LatLng(lat, lng);
        _riderMarkers.value = updated;
      } catch (e) {
        debugPrint('[Rider Telemetry Parse Error]: $e');
      }
    }, onError: (dynamic e) {
      debugPrint('[Rider WS Error]: $e');
    },);

    _wsStateSub = _wsClient!.stateStream.listen((s) {
      debugPrint('[Customer WS State]: $s');
    });

    // ── Topic Stream: Order Status Updates ──────────────────────────
    // Uses throttled topic stream to prevent UI flooding when server
    // pushes bursts of order status frames. Backward compatible:
    // untagged gateway frames are broadcast to all topic controllers.
    _wsOrderTopicSub = _wsClient!.topicStream('orders').listen((raw) {
      if (raw is! String) return;
      try {
        final frame = jsonDecode(raw) as Map<String, dynamic>;
        if (frame['action'] == 'ORDER_STATUS_UPDATED') {
          final orderId = frame['order_id']?.toString() ?? '';
          final newStatus = frame['status']?.toString() ?? '';
          if (orderId.isNotEmpty && newStatus.isNotEmpty) {
            _applyOrderStatusUpdate(orderId, newStatus);
          }
        }
      } catch (_) {}
    });

    // ── Ride Events Stream: Counter-Offers & Driver Acceptance ──────
    _wsRideEventsSub = _wsClient!.stream.listen((raw) {
      if (raw is! String) return;
      try {
        final frame = jsonDecode(raw) as Map<String, dynamic>;
        final action = frame['action']?.toString() ?? '';
        if (action == 'RIDER_COUNTER_OFFER') {
          _handleRiderCounterOffer(frame);
        } else if (action == 'RIDE_ACCEPTED') {
          _handleRideAccepted(frame);
        } else if (action == 'RIDE_STATUS_UPDATED') {
          _handleRideStatusUpdated(frame);
        } else if (action == 'RIDE_COMPLETED') {
          _handleRideCompleted(frame);
        } else if (action == 'RIDE_CANCELLED') {
          _handleRideCancelled(frame);
        }
      } catch (_) {}
    });
  }

  void _handleRiderCounterOffer(Map<String, dynamic> frame) {
    if (!mounted) return;
    final rideId = frame['tracking_id']?.toString() ?? '';
    if (_activeRideTrackingId != null && rideId != _activeRideTrackingId) return;

    final bidId = (frame['bid_id'] as num?)?.toInt() ?? 0;
    final riderTrackId = frame['rider_tracking_id']?.toString() ?? '';
    final riderName = frame['rider_name']?.toString() ?? 'Captain';
    final riderPhone = frame['rider_phone']?.toString() ?? '';
    final vehicleType = frame['vehicle_type']?.toString() ?? 'bike';
    final vehiclePlate = frame['vehicle_plate']?.toString() ?? 'LHR-LIVE';
    final bidAmount = (frame['bid_amount'] ?? frame['proposed_fare'] as num?)?.toDouble() ?? _activeRideFare;
    final rating = frame['rating']?.toString() ?? '4.8 ★';
    final eta = frame['eta']?.toString() ?? '3 min';

    setState(() {
      final exists = _liveBids.any((b) => b['bid_id'] == bidId || b['rider_tracking_id'] == riderTrackId);
      if (!exists) {
        _liveBids.add({
          'bid_id': bidId,
          'rider_tracking_id': riderTrackId,
          'rider_name': riderName,
          'rider_phone': riderPhone,
          'vehicle_type': vehicleType,
          'vehicle_plate': vehiclePlate,
          'bid_amount': bidAmount,
          'rating': rating,
          'eta': eta,
        });
        _rideSearchStatus = 'offers_received';
      }
    });

    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text('⚡ New Offer from $riderName: PKR ${bidAmount.toStringAsFixed(0)}'),
        backgroundColor: const Color(0xFF1B2A10),
        behavior: SnackBarBehavior.floating,
        duration: const Duration(seconds: 4),
      ),
    );
  }

  void _handleRideAccepted(Map<String, dynamic> frame) {
    if (!mounted) return;
    final rideId = frame['tracking_id']?.toString() ?? '';
    if (_activeRideTrackingId != null && rideId != _activeRideTrackingId) return;

    _radarSearchTimer?.cancel();
    setState(() {
      _rideSearchStatus = 'accepted';
      _acceptedRiderInfo = {
        'tracking_id': rideId,
        'rider_tracking_id': frame['rider_tracking_id']?.toString() ?? '',
        'rider_name': frame['rider_name']?.toString() ?? 'Captain',
        'rider_phone': frame['rider_phone']?.toString() ?? '',
        'vehicle_type': frame['vehicle_type']?.toString() ?? 'bike',
        'vehicle_plate': frame['vehicle_plate']?.toString() ?? 'LHR-LIVE',
        'fare_amount': (frame['fare_amount'] as num?)?.toDouble() ?? _activeRideFare,
        'status': 'accepted',
        'rating': '4.9 ★',
      };
      _liveBids.clear();
    });

    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text('🎉 Driver Accepted! ${_acceptedRiderInfo!['rider_name']} is arriving.'),
        backgroundColor: Colors.green,
        behavior: SnackBarBehavior.floating,
      ),
    );
  }

  void _handleRideStatusUpdated(Map<String, dynamic> frame) {
    if (!mounted) return;
    final rideId = frame['tracking_id']?.toString() ?? '';
    if (_activeRideTrackingId != null && rideId != _activeRideTrackingId) return;

    final status = frame['status']?.toString() ?? '';
    setState(() {
      if (_acceptedRiderInfo != null) {
        _acceptedRiderInfo!['status'] = status;
      }
    });

    String bannerText = 'Ride status updated: $status';
    if (status == 'in_progress') {
      bannerText = '🚗 Ride Started! Heading towards destination.';
    } else if (status == 'arrived') {
      bannerText = '📍 Captain has arrived at your pickup location!';
    }

    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text(bannerText),
        backgroundColor: const Color(0xFF1B2A10),
        behavior: SnackBarBehavior.floating,
        duration: const Duration(seconds: 4),
      ),
    );
  }

  void _handleRideCompleted(Map<String, dynamic> frame) {
    if (!mounted) return;
    final rideId = frame['tracking_id']?.toString() ?? '';
    if (_activeRideTrackingId != null && rideId != _activeRideTrackingId) return;

    _radarSearchTimer?.cancel();
    final double finalFare = (frame['final_fare'] as num?)?.toDouble() ?? _activeRideFare;
    final String paymentMethod = frame['payment_method']?.toString() ?? _activeRidePaymentMethod;
    final riderInfo = _acceptedRiderInfo ?? {};
    final String riderName = riderInfo['rider_name']?.toString() ?? 'Captain';
    final String vehiclePlate = riderInfo['vehicle_plate']?.toString() ?? 'LHR-LIVE';

    setState(() {
      _rideSearchStatus = 'idle';
      _activeRideTrackingId = null;
      _liveBids.clear();
      _acceptedRiderInfo = null;
      _rideRoutePolyline.clear();
      _mapMarkers.removeWhere((k, _) => k.startsWith('driver_') || k == 'pickup' || k == 'dropoff');
    });

    _showRideReceiptAndRatingSheet(rideId, finalFare, paymentMethod, riderName, vehiclePlate);
  }

  void _handleRideCancelled(Map<String, dynamic> frame) {
    if (!mounted) return;
    final rideId = frame['tracking_id']?.toString() ?? '';
    if (_activeRideTrackingId != null && rideId != _activeRideTrackingId) return;

    _radarSearchTimer?.cancel();
    setState(() {
      _rideSearchStatus = 'idle';
      _activeRideTrackingId = null;
      _liveBids.clear();
      _acceptedRiderInfo = null;
      _rideRoutePolyline.clear();
      _mapMarkers.removeWhere((k, _) => k.startsWith('driver_') || k == 'pickup' || k == 'dropoff');
    });

    final reason = frame['reason']?.toString() ?? 'Driver or Customer cancelled';
    ScaffoldMessenger.of(context).showSnackBar(
      SnackBar(
        content: Text('⚠️ Ride cancelled: $reason'),
        backgroundColor: Colors.redAccent,
        behavior: SnackBarBehavior.floating,
      ),
    );
  }

  void _showRideReceiptAndRatingSheet(
    String rideId,
    double fare,
    String paymentMethod,
    String riderName,
    String vehiclePlate,
  ) {
    unawaited(showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Colors.transparent,
      barrierColor: Colors.black87,
      builder: (context) {
        double selectedRating = 5.0;
        double selectedTip = 0.0;
        final TextEditingController commentController = TextEditingController();

        return StatefulBuilder(
          builder: (context, setSheetState) {
            return Padding(
              padding: EdgeInsets.only(
                bottom: MediaQuery.of(context).viewInsets.bottom,
              ),
              child: ClipRRect(
                borderRadius: const BorderRadius.vertical(top: Radius.circular(36)),
                child: BackdropFilter(
                  filter: ImageFilter.blur(sigmaX: 20, sigmaY: 20),
                  child: Container(
                    padding: const EdgeInsets.fromLTRB(24, 20, 24, 30),
                    decoration: BoxDecoration(
                      color: const Color(0xFF141414),
                      borderRadius: const BorderRadius.vertical(top: Radius.circular(36)),
                      border: Border.all(color: Colors.white.withOpacity(0.12)),
                    ),
                    child: Column(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        Container(
                          width: 48,
                          height: 5,
                          decoration: BoxDecoration(
                            color: Colors.white24,
                            borderRadius: BorderRadius.circular(10),
                          ),
                        ),
                        const SizedBox(height: 20),
                        const CircleAvatar(
                          radius: 28,
                          backgroundColor: Color(0xFFCAFF33),
                          child: Icon(Icons.check_rounded, color: Colors.black, size: 36),
                        ),
                        const SizedBox(height: 12),
                        const Text(
                          'Trip Completed!',
                          style: TextStyle(
                            color: Colors.white,
                            fontSize: 22,
                            fontWeight: FontWeight.bold,
                            fontFamily: 'Outfit',
                          ),
                        ),
                        const SizedBox(height: 6),
                        Text(
                          'PKR ${fare.toStringAsFixed(0)}',
                          style: const TextStyle(
                            color: Color(0xFFCAFF33),
                            fontSize: 32,
                            fontWeight: FontWeight.w900,
                            fontFamily: 'Outfit',
                          ),
                        ),
                        const SizedBox(height: 8),
                        Container(
                          padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 6),
                          decoration: BoxDecoration(
                            color: Colors.white.withOpacity(0.08),
                            borderRadius: BorderRadius.circular(20),
                          ),
                          child: Row(
                            mainAxisSize: MainAxisSize.min,
                            children: [
                              Icon(
                                paymentMethod == 'wallet'
                                    ? Icons.account_balance_wallet_outlined
                                    : (paymentMethod == 'card' || paymentMethod == 'safepay'
                                        ? Icons.credit_card_outlined
                                        : Icons.payments_outlined),
                                color: Colors.white70,
                                size: 16,
                              ),
                              const SizedBox(width: 6),
                              Text(
                                paymentMethod == 'wallet'
                                    ? 'Deducted from OMNIGO Wallet'
                                    : (paymentMethod == 'card' || paymentMethod == 'safepay'
                                        ? 'Paid Online via Card / Safepay'
                                        : 'Paid in Cash to Captain'),
                                style: const TextStyle(color: Colors.white70, fontSize: 13, fontWeight: FontWeight.bold),
                              ),
                            ],
                          ),
                        ),
                        if (paymentMethod == 'cash') ...[
                          const SizedBox(height: 6),
                          InkWell(
                            onTap: () {
                              setSheetState(() {
                                paymentMethod = 'safepay';
                              });
                            },
                            child: const Text(
                              'Out of cash? Pay online via Card / Safepay ➔',
                              style: TextStyle(
                                color: Color(0xFFCAFF33),
                                fontSize: 11,
                                fontWeight: FontWeight.bold,
                              ),
                            ),
                          ),
                        ],
                        const SizedBox(height: 20),
                        const Divider(color: Colors.white10),
                        const SizedBox(height: 14),
                        Text(
                          'Rate Captain $riderName ($vehiclePlate)',
                          style: const TextStyle(color: Colors.white, fontWeight: FontWeight.bold, fontSize: 15),
                        ),
                        const SizedBox(height: 10),
                        Row(
                          mainAxisAlignment: MainAxisAlignment.center,
                          children: List.generate(5, (index) {
                            final star = index + 1;
                            return IconButton(
                              onPressed: () => setSheetState(() => selectedRating = star.toDouble()),
                              icon: Icon(
                                star <= selectedRating ? Icons.star_rounded : Icons.star_outline_rounded,
                                color: Colors.amber,
                                size: 36,
                              ),
                            );
                          }),
                        ),
                        const SizedBox(height: 14),
                        Row(
                          mainAxisAlignment: MainAxisAlignment.center,
                          children: [
                            const Text('Add Tip: ', style: TextStyle(color: Colors.white60, fontSize: 13)),
                            const SizedBox(width: 8),
                            for (final tip in [20.0, 50.0, 100.0])
                              Padding(
                                padding: const EdgeInsets.symmetric(horizontal: 4),
                                child: ChoiceChip(
                                  label: Text('+ PKR ${tip.toStringAsFixed(0)}'),
                                  selected: selectedTip == tip,
                                  onSelected: (sel) => setSheetState(() => selectedTip = sel ? tip : 0.0),
                                  selectedColor: const Color(0xFFCAFF33),
                                  backgroundColor: Colors.white10,
                                  labelStyle: TextStyle(
                                    color: selectedTip == tip ? Colors.black : Colors.white70,
                                    fontWeight: FontWeight.bold,
                                    fontSize: 11,
                                  ),
                                ),
                              ),
                          ],
                        ),
                        const SizedBox(height: 14),
                        TextField(
                          controller: commentController,
                          style: const TextStyle(color: Colors.white, fontSize: 13),
                          decoration: InputDecoration(
                            hintText: 'Leave a note for the captain (optional)',
                            hintStyle: const TextStyle(color: Colors.white38, fontSize: 13),
                            filled: true,
                            fillColor: Colors.white.withOpacity(0.05),
                            contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 12),
                            border: OutlineInputBorder(
                              borderRadius: BorderRadius.circular(16),
                              borderSide: BorderSide.none,
                            ),
                          ),
                        ),
                        const SizedBox(height: 24),
                        ElevatedButton(
                          onPressed: () async {
                            Navigator.pop(context);
                            try {
                              await sl<ApiClient>().post(ApiEndpoints.rideRate(rideId), {
                                'rating': selectedRating,
                                'comment': commentController.text.trim(),
                                'tip_amount': selectedTip,
                              });
                            } catch (_) {}
                            if (mounted) {
                              ScaffoldMessenger.of(context).showSnackBar(
                                const SnackBar(
                                  content: Text('⭐ Thank you for rating your ride!'),
                                  backgroundColor: Color(0xFFCAFF33),
                                  behavior: SnackBarBehavior.floating,
                                ),
                              );
                            }
                          },
                          style: ElevatedButton.styleFrom(
                            backgroundColor: const Color(0xFFCAFF33),
                            foregroundColor: Colors.black,
                            minimumSize: const Size(double.infinity, 54),
                            shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(18)),
                          ),
                          child: const Text('Submit & Done', style: TextStyle(fontWeight: FontWeight.w900, fontSize: 16)),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            );
          },
        );
      },
    ),);
  }

  /// Applies a real-time order status update received via WebSocket.
  /// Updates the local order list without requiring an HTTP refresh.
  void _applyOrderStatusUpdate(String orderId, String newStatus) {
    if (!mounted) return;
    setState(() {
      for (var i = 0; i < _customerOrders.length; i++) {
        if ((_customerOrders[i]['order_tracking_id']?.toString() ?? _customerOrders[i]['id']?.toString()) == orderId) {
          _customerOrders[i] = Map<String, dynamic>.from(_customerOrders[i] as Map) ..['status'] = newStatus;
          break;
        }
      }

      // H4: If the active delivery order reached a terminal state, clear the route.
      if (_activeDeliveryOrderId != null && orderId == _activeDeliveryOrderId) {
        if (['delivered', 'completed', 'cancelled', 'failed'].contains(newStatus)) {
          _deliveryRoutePolyline = [];
          _activeDeliveryOrderId = null;
          _deliveryEtaSeconds = null;
          _deliveryDistanceMeters = null;
          _mapMarkers.remove('destination');
        }
      }
    });

    // H4: When order transitions to shipped/in_transit, fetch route polyline for the map.
    if (['shipped', 'in_transit'].contains(newStatus)) {
      final hasRider = _customerOrders.any(
        (o) => (o['rider_tracking_id'] as String?)?.isNotEmpty == true,
      );
      if (hasRider && (_activeDeliveryOrderId == null || _activeDeliveryOrderId != orderId)) {
        _activeDeliveryOrderId = orderId;
        _fetchDeliveryRoute(orderId);
      }
    }

    debugPrint('[OrderWS] Order $orderId status updated to $newStatus (real-time)');
  }

  /// Resets pagination and fetches a fresh page from the backend using the
  /// current `_searchQuery` and `_selectedCategory` as server-side filters.
  /// This replaces the old client-side `.where()` filter — the Go
  /// product-service already supports `?search=` and `?category=` query
  /// params via ILIKE + exact category match.
  final String _selectedSort = 'newest';
  final double _minPriceFilter = 0.0;
  final double _maxPriceFilter = 50000.0;

  Future<void> _fetchCatalog({bool reset = false}) async {
    if (_isLoadingCatalog) return;
    if (reset) {
      _offset = 0;
      _hasMore = true;
      _allProducts = [];
    }
    if (!_hasMore) return;

    setState(() => _isLoadingCatalog = true);

    try {
      final api = sl<ApiClient>();
      final url = ApiEndpoints.productsList(
        limit: _limit,
        offset: _offset,
        search: _searchQuery,
        category: _selectedCategory,
        sort: _selectedSort,
        minPrice: _minPriceFilter,
        maxPrice: _maxPriceFilter,
      );
      final response = await api.get(url);

      if (response is List) {
        if (mounted) {
          setState(() {
            if (response.isEmpty) {
              _hasMore = false;
            } else {
              _allProducts.addAll(response.map((e) => Product.fromJson(e as Map<String, dynamic>)).toList());
              _offset += _limit;
            }
            _isLoadingCatalog = false;
          });
        }
      } else {
        // Response is not a List — unexpected format, reset loading state
        if (mounted) {
          setState(() {
            _isLoadingCatalog = false;
          });
          debugPrint('Unexpected catalog response format: ${response.runtimeType}');
        }
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isLoadingCatalog = false;
        });
        debugPrint('Error fetching catalog: $e');
      }
    }
  }

  /// Debounced server-side search trigger. Called from the search TextField.
  void _onSearchChanged(String val) {
    setState(() => _searchQuery = val);
    _searchDebounce?.cancel();
    _searchDebounce = Timer(const Duration(milliseconds: 400), () {
      _fetchCatalog(reset: true);
    });
  }

  /// Server-side category filter trigger. Called from category pills.
  void _onCategorySelected(String cat) {
    setState(() => _selectedCategory = cat);
    _fetchCatalog(reset: true);
  }

  /// Geocodes a free-text place query via the open Nominatim OSM API.
  ///
  /// Nominatim is free and requires no API key, but mandates a descriptive
  /// `User-Agent` header (per OSM Tile Usage Policy). We fall back to the
  /// local mock DB if the network call fails — keeps the UX responsive
  /// even offline.
  Future<void> _searchMap(String query) async {
    if (query.isEmpty) return;
    final q = query.trim();

    setState(() => _isGeocoding = true);

    LatLng? coords;
    String? displayName;

    try {
      // Geocoding via secure OMNIGO Backend Proxy
      final api = sl<ApiClient>();
      final response = await api.get(ApiEndpoints.geocodingSearch(q));

      if (response is List && response.isNotEmpty) {
        final hit = response.first as Map<String, dynamic>;
        final lat = double.tryParse(hit['lat']?.toString() ?? '') ?? 0.0;
        final lon = double.tryParse(hit['lon']?.toString() ?? '') ?? 0.0;
        if (lat != 0.0 || lon != 0.0) {
            coords = LatLng(lat, lon);
            displayName = hit['display_name']?.toString();
          }
      }
    } catch (e) {
      debugPrint('Nominatim geocoding failed: $e');
    }

    setState(() => _isGeocoding = false);

    if (coords == null) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
              content: Text(
                  'No location found for "$q". Try a more specific name.',),),
        );
      }
      return;
    }

    _mapController?.animateCamera(
      CameraUpdate.newLatLngZoom(coords, 14.0),
    );
    _rideDropoffLatLng = coords;

    if (mounted && displayName != null) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(
          content: Text('Location: $displayName'),
          duration: const Duration(seconds: 4),
          action: SnackBarAction(
            label: 'Book Ride',
            onPressed: () => _estimateAndSelectVehicle(),
          ),
        ),
      );
    }
  }

  Future<void> _estimateAndSelectVehicle() async {
    if (_rideDropoffLatLng == null) return;
    // #33: Race condition guard
    if (_isEstimatingRide) return;

    setState(() => _isEstimatingRide = true);

    // #31: Safety timeout — store timer and cancel in dispose
    _estimateSafetyTimer?.cancel();
    _estimateSafetyTimer = Timer(const Duration(seconds: 20), () {
      if (mounted && _isEstimatingRide) {
        setState(() => _isEstimatingRide = false);
      }
    });

    LatLng? pickup;
    const maxRetries = 3;
    String? gpsError;

    for (int attempt = 0; attempt < maxRetries; attempt++) {
      try {
        final bool serviceEnabled = await Geolocator.isLocationServiceEnabled();
        if (!serviceEnabled) {
          gpsError = 'Location services disabled. Enable GPS.';
          break;
        }
        LocationPermission permission = await Geolocator.checkPermission();
        if (permission == LocationPermission.denied) {
          permission = await Geolocator.requestPermission();
          if (permission == LocationPermission.denied) {
            if (attempt < maxRetries - 1) {
              await Future<void>.delayed(Duration(seconds: attempt + 1));
              continue;
            }
            gpsError = 'Location permission denied.';
            break;
          }
        }
        if (permission == LocationPermission.deniedForever) {
          gpsError = 'Location permanently denied. Enable in Settings.';
          break;
        }
        final position = await Geolocator.getCurrentPosition(
          locationSettings: const LocationSettings(accuracy: LocationAccuracy.high),
        ).timeout(const Duration(seconds: 10));
        pickup = LatLng(position.latitude, position.longitude);
        break;
      } catch (e) {
        if (attempt < maxRetries - 1) {
          await Future<void>.delayed(Duration(seconds: attempt + 1));
          continue;
        }
        gpsError = 'Could not get location. Please enable GPS.';
      }
    }

    if (pickup == null) {
      if (mounted && gpsError != null) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text(gpsError),
            backgroundColor: Colors.red,
            behavior: SnackBarBehavior.floating,
          ),
        );
      }
      return;
    }

    _ridePickupLatLng = pickup;

    // Update map markers
    setState(() {
      _mapMarkers.clear();
      _mapMarkers['pickup'] = MarkerData(
        position: _ridePickupLatLng!,
        iconSize: 1.0,
      );
      _mapMarkers['dropoff'] = MarkerData(
        position: _rideDropoffLatLng!,
        iconSize: 1.0,
      );
    });

    try {
      final response = await sl<ApiClient>().post(ApiEndpoints.rideEstimate(), {
        'pickup_lat': _ridePickupLatLng!.latitude,
        'pickup_lng': _ridePickupLatLng!.longitude,
        'dropoff_lat': _rideDropoffLatLng!.latitude,
        'dropoff_lng': _rideDropoffLatLng!.longitude,
        'vehicle_type': '',
      });

      final List<dynamic> estimates = (response['estimates'] as List<dynamic>?) ?? <dynamic>[];
      final List<dynamic> geometry = (response['geometry'] as List<dynamic>?) ?? <dynamic>[];

      if (mounted) {
        setState(() {
          _isEstimatingRide = false;
          // OSRM coordinates are [lng, lat], map to LatLng(lat, lng)
          _rideRoutePolyline = geometry.map<LatLng>((c) {
            final double lng = (c[0] as num).toDouble();
            final double lat = (c[1] as num).toDouble();
            return LatLng(lat, lng);
          }).toList();
        });

        // Trigger premium selector sheet overlay
        unawaited(showModalBottomSheet<void>(
          context: context,
          isScrollControlled: true,
          backgroundColor: Colors.transparent,
          barrierColor: Colors.black54,
          builder: (context) {
            return VehicleSelectorSheet(
              estimates: estimates,
              onBookRide: (type, fare, method) => _bookRideConfirmation(type, fare, method),
            );
          },
        ),);
      }
    } catch (e) {
      setState(() => _isEstimatingRide = false);
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Fare estimation failed: $e')),
        );
      }
    }
  }

  void _bookRideConfirmation(String vehicleType, double fare, String paymentMethod) {
    _requestRide(vehicleType, fare, paymentMethod);
  }

  Future<void> _requestRide(String vehicleType, double fare, String paymentMethod) async {
    if (_ridePickupLatLng == null || _rideDropoffLatLng == null) return;
    _activeRidePaymentMethod = paymentMethod;

    unawaited(showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (context) => const Center(
        child: CircularProgressIndicator(color: Color(0xFFCAFF33)),
      ),
    ),);

    try {
      final response = await sl<ApiClient>().post(ApiEndpoints.rideCreate(), {
        'customer_tracking_id': widget.trackingId,
        'vehicle_type': vehicleType,
        'pickup_lat': _ridePickupLatLng!.latitude,
        'pickup_lng': _ridePickupLatLng!.longitude,
        'dropoff_lat': _rideDropoffLatLng!.latitude,
        'dropoff_lng': _rideDropoffLatLng!.longitude,
        'fare_amount': fare,
        'payment_method': paymentMethod,
      });

      if (mounted) {
        Navigator.pop(context); // pop spinner
        final rideData = response is Map<String, dynamic> ? response : <String, dynamic>{};
        _startRideRadarSearch(rideData, vehicleType, fare);
      }
    } catch (e) {
      if (mounted) {
        Navigator.pop(context); // pop spinner
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to request ride: $e')),
        );
      }
    }
  }

  void _startRideRadarSearch(Map<String, dynamic> rideData, String vehicleType, double fare) {
    final trackingId = rideData['tracking_id']?.toString() ?? '';
    final nearbyCount = (rideData['nearby_riders_count'] as num?)?.toInt() ?? 3;
    final searchRadius = (rideData['search_radius_km'] as num?)?.toDouble() ?? 3.0;

    setState(() {
      _activeRideTrackingId = trackingId;
      _rideSearchStatus = 'searching';
      _activeRideFare = fare;
      _activeRideVehicleType = vehicleType;
      _nearbyRidersCount = nearbyCount > 0 ? nearbyCount : 3;
      _searchRadiusKm = searchRadius;
      _searchElapsedSeconds = 0;
      _liveBids = [];
      _acceptedRiderInfo = null;

      // Keep pickup and dropoff markers pinned
      if (_ridePickupLatLng != null) {
        _mapMarkers['pickup'] = MarkerData(position: _ridePickupLatLng!, iconSize: 1.2);
      }
      if (_rideDropoffLatLng != null) {
        _mapMarkers['dropoff'] = MarkerData(position: _rideDropoffLatLng!, iconSize: 1.2);
      }
    });

    // Pan camera to pickup location
    if (_mapController != null && _ridePickupLatLng != null) {
      _mapController!.animateCamera(
        CameraUpdate.newLatLngZoom(_ridePickupLatLng!, 14.5),
      );
    }

    // Discover nearby drivers and mark on map
    if (_ridePickupLatLng != null) {
      _fetchNearbyDriversForMap(_ridePickupLatLng!.latitude, _ridePickupLatLng!.longitude);
    }

    // Start expanding search radar timer
    _radarSearchTimer?.cancel();
    _radarSearchTimer = Timer.periodic(const Duration(seconds: 1), (timer) {
      if (!mounted || (_rideSearchStatus != 'searching' && _rideSearchStatus != 'offers_received')) {
        timer.cancel();
        return;
      }
      setState(() {
        _searchElapsedSeconds++;
        if (_searchElapsedSeconds % 4 == 0 && _searchRadiusKm < 8.0) {
          _searchRadiusKm += 0.5;
        }
      });
    });
  }

  Future<void> _fetchNearbyDriversForMap(double lat, double lng) async {
    try {
      final resp = await sl<ApiClient>().get(ApiEndpoints.rideNearby(lat, lng));
      if (resp is Map<String, dynamic> && resp['riders'] is List) {
        final List<dynamic> riders = resp['riders'] as List<dynamic>;
        if (!mounted) return;
        setState(() {
          if (riders.isNotEmpty) {
            _nearbyRidersCount = riders.length;
          }
          for (var i = 0; i < riders.length; i++) {
            final r = riders[i] as Map<String, dynamic>;
            final rLat = (r['latitude'] as num?)?.toDouble();
            final rLng = (r['longitude'] as num?)?.toDouble();
            final rId = r['rider_tracking_id']?.toString() ?? 'r_$i';
            if (rLat != null && rLng != null) {
              _mapMarkers['driver_$rId'] = MarkerData(
                position: LatLng(rLat, rLng),
                iconSize: 0.9,
              );
            }
          }
        });
      }
    } catch (e) {
      debugPrint('[NearbyDrivers] Error: $e');
    }
  }

  Future<void> _bumpFare(double increaseAmount) async {
    if (_activeRideTrackingId == null) return;
    try {
      final resp = await sl<ApiClient>().post(ApiEndpoints.rideBumpFare(_activeRideTrackingId!), {
        'increase_amount': increaseAmount,
      });
      final newFare = (resp is Map && resp['fare_amount'] is num)
          ? (resp['fare_amount'] as num).toDouble()
          : (_activeRideFare + increaseAmount);
      setState(() {
        _activeRideFare = newFare;
      });
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('⚡ Fare increased to PKR ${newFare.toStringAsFixed(0)}! Re-broadcasting to drivers...'),
            backgroundColor: const Color(0xFFCAFF33),
            behavior: SnackBarBehavior.floating,
            duration: const Duration(seconds: 2),
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to bump fare: $e')),
        );
      }
    }
  }

  Future<void> _acceptBid(Map<String, dynamic> bid) async {
    if (_activeRideTrackingId == null) return;
    final bidId = bid['bid_id'];
    try {
      await sl<ApiClient>().post(ApiEndpoints.rideAcceptBid(_activeRideTrackingId!), {
        'bid_id': bidId,
      });
      _radarSearchTimer?.cancel();
      setState(() {
        _rideSearchStatus = 'accepted';
        _acceptedRiderInfo = {
          'tracking_id': _activeRideTrackingId,
          'rider_tracking_id': bid['rider_tracking_id'],
          'rider_name': bid['rider_name'],
          'rider_phone': bid['rider_phone'],
          'vehicle_type': bid['vehicle_type'],
          'vehicle_plate': bid['vehicle_plate'],
          'fare_amount': bid['bid_amount'],
          'status': 'accepted',
          'rating': bid['rating'] ?? '4.8 ★',
        };
        _liveBids.clear();
      });
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Failed to accept bid: $e')),
        );
      }
    }
  }

  Future<void> _cancelRide() async {
    if (_activeRideTrackingId != null) {
      try {
        await sl<ApiClient>().post(ApiEndpoints.rideCancel(_activeRideTrackingId!), {
          'actor_role': 'customer',
        });
      } catch (_) {}
    }
    _radarSearchTimer?.cancel();
    setState(() {
      _rideSearchStatus = 'idle';
      _activeRideTrackingId = null;
      _liveBids.clear();
      _acceptedRiderInfo = null;
      _rideRoutePolyline.clear();
      _mapMarkers.removeWhere((k, _) => k.startsWith('driver_') || k == 'pickup' || k == 'dropoff');
    });
    if (mounted) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(
          content: Text('Ride request cancelled.'),
          behavior: SnackBarBehavior.floating,
          duration: Duration(seconds: 2),
        ),
      );
    }
  }

  Widget _buildRideRadarPanel() {
    if (_rideSearchStatus == 'accepted' && _acceptedRiderInfo != null) {
      return Container(
        padding: const EdgeInsets.all(20),
        decoration: BoxDecoration(
          color: const Color(0xFF141414),
          borderRadius: BorderRadius.circular(24),
          border: Border.all(color: const Color(0xFFCAFF33).withOpacity(0.4)),
          boxShadow: [
            BoxShadow(
              color: Colors.black.withOpacity(0.6),
              blurRadius: 20,
              offset: const Offset(0, 8),
            ),
          ],
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                  decoration: BoxDecoration(
                    color: const Color(0xFFCAFF33).withOpacity(0.2),
                    borderRadius: BorderRadius.circular(12),
                  ),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(
                        _acceptedRiderInfo!['status'] == 'in_progress' 
                            ? Icons.navigation_rounded 
                            : (_acceptedRiderInfo!['status'] == 'arrived' ? Icons.location_on_rounded : Icons.check_circle_rounded),
                        color: const Color(0xFFCAFF33),
                        size: 16,
                      ),
                      const SizedBox(width: 6),
                      Text(
                        _acceptedRiderInfo!['status'] == 'in_progress'
                            ? 'Ride in Progress • In Transit'
                            : (_acceptedRiderInfo!['status'] == 'arrived'
                                ? 'Captain Arrived at Pickup'
                                : 'Driver Found & On The Way'),
                        style: const TextStyle(
                          color: Color(0xFFCAFF33),
                          fontWeight: FontWeight.bold,
                          fontSize: 12,
                          fontFamily: 'Outfit',
                        ),
                      ),
                    ],
                  ),
                ),
                const Spacer(),
                Column(
                  crossAxisAlignment: CrossAxisAlignment.end,
                  children: [
                    Text(
                      'PKR ${_acceptedRiderInfo!['fare_amount'] != null ? (_acceptedRiderInfo!['fare_amount'] as num).toStringAsFixed(0) : _activeRideFare.toStringAsFixed(0)}',
                      style: const TextStyle(
                        color: Color(0xFFCAFF33),
                        fontWeight: FontWeight.w900,
                        fontSize: 20,
                        fontFamily: 'Outfit',
                      ),
                    ),
                    Text(
                      _activeRidePaymentMethod == 'wallet' ? '👛 Wallet' : '💵 Cash',
                      style: const TextStyle(color: Colors.white60, fontSize: 11, fontWeight: FontWeight.bold),
                    ),
                  ],
                ),
              ],
            ),
            const SizedBox(height: 14),
            Row(
              children: [
                CircleAvatar(
                  radius: 24,
                  backgroundColor: Colors.white12,
                  child: Icon(
                    _activeRideVehicleType == 'bike' ? Icons.two_wheeler_rounded : Icons.directions_car_rounded,
                    color: Colors.white,
                    size: 26,
                  ),
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        _acceptedRiderInfo!['rider_name']?.toString() ?? 'Captain',
                        style: const TextStyle(
                          color: Colors.white,
                          fontWeight: FontWeight.bold,
                          fontSize: 17,
                          fontFamily: 'Outfit',
                        ),
                      ),
                      const SizedBox(height: 2),
                      Row(
                        children: [
                          Container(
                            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                            decoration: BoxDecoration(
                              color: Colors.white10,
                              borderRadius: BorderRadius.circular(6),
                            ),
                            child: Text(
                              _acceptedRiderInfo!['vehicle_plate']?.toString() ?? 'LHR-LIVE',
                              style: const TextStyle(color: Colors.white70, fontSize: 11, fontWeight: FontWeight.bold),
                            ),
                          ),
                          const SizedBox(width: 8),
                          Text(
                            _acceptedRiderInfo!['rating']?.toString() ?? '4.8 ★',
                            style: const TextStyle(color: Colors.amber, fontSize: 12, fontWeight: FontWeight.bold),
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
                if ((_acceptedRiderInfo!['rider_phone']?.toString() ?? '').isNotEmpty)
                  IconButton(
                    onPressed: () {
                      final phone = _acceptedRiderInfo!['rider_phone'].toString();
                      unawaited(launchUrl(Uri.parse('tel:$phone')));
                    },
                    icon: const CircleAvatar(
                      backgroundColor: Color(0xFFCAFF33),
                      radius: 20,
                      child: Icon(Icons.phone, color: Colors.black, size: 20),
                    ),
                  ),
              ],
            ),
            const SizedBox(height: 16),
            if (_acceptedRiderInfo!['status'] != 'in_progress')
              SizedBox(
                width: double.infinity,
                child: OutlinedButton(
                  onPressed: _cancelRide,
                  style: OutlinedButton.styleFrom(
                    foregroundColor: Colors.redAccent,
                    side: BorderSide(color: Colors.redAccent.withOpacity(0.5)),
                    shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
                    padding: const EdgeInsets.symmetric(vertical: 12),
                  ),
                  child: const Text('Cancel Ride', style: TextStyle(fontFamily: 'Outfit', fontWeight: FontWeight.bold)),
                ),
              ),
          ],
        ),
      );
    }

    // Searching / Offers Received Radar View
    final minutes = _searchElapsedSeconds ~/ 60;
    final seconds = _searchElapsedSeconds % 60;
    final timeStr = '${minutes.toString().padLeft(2, '0')}:${seconds.toString().padLeft(2, '0')}';

    return Container(
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: const Color(0xFF141414),
        borderRadius: BorderRadius.circular(24),
        border: Border.all(color: Colors.white.withOpacity(0.12)),
        boxShadow: [
          BoxShadow(
            color: Colors.black.withOpacity(0.6),
            blurRadius: 20,
            offset: const Offset(0, 10),
          ),
        ],
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Radar Header
          Row(
            children: [
              Container(
                width: 36,
                height: 36,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: const Color(0xFFCAFF33).withOpacity(0.15),
                ),
                child: const Center(
                  child: SizedBox(
                    width: 20,
                    height: 20,
                    child: CircularProgressIndicator(
                      strokeWidth: 2.5,
                      color: Color(0xFFCAFF33),
                    ),
                  ),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Text(
                      'Pinging Nearby Drivers...',
                      style: TextStyle(
                        color: Colors.white,
                        fontWeight: FontWeight.bold,
                        fontSize: 16,
                        fontFamily: 'Outfit',
                      ),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      'Broadcasting to $_nearbyRidersCount drivers in ${_searchRadiusKm.toStringAsFixed(1)} km',
                      style: const TextStyle(
                        color: Colors.white60,
                        fontSize: 12,
                        fontFamily: 'Outfit',
                      ),
                    ),
                  ],
                ),
              ),
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
                decoration: BoxDecoration(
                  color: Colors.white.withOpacity(0.08),
                  borderRadius: BorderRadius.circular(12),
                ),
                child: Text(
                  timeStr,
                  style: const TextStyle(
                    color: Color(0xFFCAFF33),
                    fontWeight: FontWeight.w900,
                    fontSize: 13,
                    fontFamily: 'Outfit',
                  ),
                ),
              ),
            ],
          ),

          const SizedBox(height: 14),

          // Current Offer & Vehicle Banner
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
            decoration: BoxDecoration(
              color: Colors.white.withOpacity(0.05),
              borderRadius: BorderRadius.circular(14),
            ),
            child: Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Row(
                  children: [
                    Icon(
                      _activeRideVehicleType == 'bike' ? Icons.two_wheeler_rounded : Icons.directions_car_rounded,
                      color: const Color(0xFFCAFF33),
                      size: 20,
                    ),
                    const SizedBox(width: 8),
                    Text(
                      _activeRideVehicleType.toUpperCase(),
                      style: const TextStyle(color: Colors.white, fontWeight: FontWeight.bold, fontSize: 13),
                    ),
                  ],
                ),
                Text(
                  'Your Fare: PKR ${_activeRideFare.toStringAsFixed(0)}',
                  style: const TextStyle(
                    color: Color(0xFFCAFF33),
                    fontWeight: FontWeight.w900,
                    fontSize: 15,
                    fontFamily: 'Outfit',
                  ),
                ),
              ],
            ),
          ),

          // Live Driver Counter Offers
          if (_liveBids.isNotEmpty) ...[
            const SizedBox(height: 14),
            Text(
              'Rider Offers (${_liveBids.length})',
              style: const TextStyle(
                color: Colors.white,
                fontWeight: FontWeight.bold,
                fontSize: 13,
                fontFamily: 'Outfit',
              ),
            ),
            const SizedBox(height: 8),
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 140),
              child: ListView.separated(
                shrinkWrap: true,
                itemCount: _liveBids.length,
                separatorBuilder: (_, __) => const SizedBox(height: 8),
                itemBuilder: (context, idx) {
                  final bid = _liveBids[idx];
                  return Container(
                    padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                    decoration: BoxDecoration(
                      color: Colors.white.withOpacity(0.08),
                      borderRadius: BorderRadius.circular(14),
                      border: Border.all(color: const Color(0xFFCAFF33).withOpacity(0.4)),
                    ),
                    child: Row(
                      children: [
                        CircleAvatar(
                          radius: 16,
                          backgroundColor: Colors.white12,
                          child: Text(
                            bid['rider_name'].toString().substring(0, 1),
                            style: const TextStyle(color: Colors.white, fontWeight: FontWeight.bold),
                          ),
                        ),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Column(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Text(
                                bid['rider_name'].toString(),
                                style: const TextStyle(color: Colors.white, fontWeight: FontWeight.bold, fontSize: 13),
                              ),
                              Text(
                                '${bid['vehicle_plate']} • ${bid['rating']} • ${bid['eta']}',
                                style: const TextStyle(color: Colors.white60, fontSize: 11),
                              ),
                            ],
                          ),
                        ),
                        Text(
                          'PKR ${(bid['bid_amount'] as num).toStringAsFixed(0)}',
                          style: const TextStyle(color: Color(0xFFCAFF33), fontWeight: FontWeight.bold, fontSize: 14),
                        ),
                        const SizedBox(width: 8),
                        ElevatedButton(
                          onPressed: () => _acceptBid(bid),
                          style: ElevatedButton.styleFrom(
                            backgroundColor: const Color(0xFFCAFF33),
                            foregroundColor: Colors.black,
                            padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                            minimumSize: Size.zero,
                            shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10)),
                          ),
                          child: const Text('Accept', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 12)),
                        ),
                      ],
                    ),
                  );
                },
              ),
            ),
          ],

          const SizedBox(height: 14),

          // Fare Bump Buttons (inDrive / Bykea style)
          Row(
            children: [
              const Text(
                'Raise Fare:',
                style: TextStyle(color: Colors.white70, fontSize: 12, fontWeight: FontWeight.bold, fontFamily: 'Outfit'),
              ),
              const SizedBox(width: 8),
              _buildFareBumpChip(20),
              const SizedBox(width: 6),
              _buildFareBumpChip(50),
              const SizedBox(width: 6),
              _buildFareBumpChip(100),
            ],
          ),

          const SizedBox(height: 12),

          // Cancel Request Button
          SizedBox(
            width: double.infinity,
            child: TextButton(
              onPressed: _cancelRide,
              style: TextButton.styleFrom(
                foregroundColor: Colors.redAccent,
                padding: const EdgeInsets.symmetric(vertical: 8),
              ),
              child: const Text('Cancel Request', style: TextStyle(fontWeight: FontWeight.bold, fontFamily: 'Outfit')),
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildFareBumpChip(double amount) {
    return InkWell(
      onTap: () => _bumpFare(amount),
      borderRadius: BorderRadius.circular(12),
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
        decoration: BoxDecoration(
          color: Colors.white.withOpacity(0.08),
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: const Color(0xFFCAFF33).withOpacity(0.4)),
        ),
        child: Text(
          '+ PKR ${amount.toStringAsFixed(0)}',
          style: const TextStyle(
            color: Color(0xFFCAFF33),
            fontSize: 11,
            fontWeight: FontWeight.bold,
            fontFamily: 'Outfit',
          ),
        ),
      ),
    );
  }

  Future<void> _fetchOrders() async {
    if (_isLoadingOrders) return;
    setState(() {
      _isLoadingOrders = true;
    });

    try {
      final response =
          await sl<ApiClient>().get(ApiEndpoints.customerOrders(widget.trackingId));
      if (mounted) {
        // #53: Verify response is List before casting
        final List<dynamic> orders = response is List<dynamic> ? response : <dynamic>[];
        setState(() {
          _customerOrders = orders;
          _isLoadingOrders = false;
        });

        // Connect WebSocket for any active order to receive real-time
        // status updates and live rider GPS telemetry.
        final activeStatuses = {'pending', 'paid', 'accepted', 'shipped', 'in_transit'};
        final hasActiveOrder = _customerOrders.any(
          (o) => activeStatuses.contains(o['status']?.toString() ?? ''),
        );
        if (hasActiveOrder) {
          _connectRiderTelemetry();
        }

        // H4: Find first active delivery order with a rider and load route polyline.
        final deliveryOrder = _customerOrders.cast<Map<String, dynamic>?>().firstWhere(
          (o) {
            final status = o?['status']?.toString() ?? '';
            final hasRider = (o?['rider_tracking_id'] as String?)?.isNotEmpty == true;
            return hasRider && (status == 'shipped' || status == 'in_transit');
          },
          orElse: () => null,
        );

        if (deliveryOrder != null) {
          final orderId = deliveryOrder['order_tracking_id']?.toString() ??
              deliveryOrder['id']?.toString() ?? '';
          if (orderId.isNotEmpty) {
            _activeDeliveryOrderId = orderId;
            unawaited(_fetchDeliveryRoute(orderId));
          }
        } else {
          setState(() {
            _deliveryRoutePolyline = [];
            _activeDeliveryOrderId = null;
            _deliveryEtaSeconds = null;
            _deliveryDistanceMeters = null;
            _mapMarkers.remove('destination');
          });
        }
      }
    } catch (e) {
      if (mounted) {
        setState(() {
          _isLoadingOrders = false;
        });
        debugPrint('Error fetching customer orders: $e');
      }
    }
  }

  Future<void> _fetchDeliveryRoute(String orderId) async {
    try {
      final response = await sl<ApiClient>().get(
        ApiEndpoints.deliveryGigRouteCustomer(orderId),
      );
      if (response is! Map<String, dynamic>) return;

      final route = response['route'] as Map<String, dynamic>?;
      if (route == null) return;

      final coords = route['coordinates'] as List<dynamic>?;
      final distance = route['distance_meters'] as num?;
      final duration = route['duration_seconds'] as num?;

      if (coords == null || coords.isEmpty) return;

      final List<LatLng> points = coords.map<LatLng>((c) {
        if (c is! List || c.length < 2) return const LatLng(0, 0);
        final lng = (c[0] as num).toDouble();
        final lat = (c[1] as num).toDouble();
        if (lng == 0 && lat == 0) return const LatLng(0, 0);
        return LatLng(lat, lng);
      }).toList();

      if (points.isEmpty || (points.length == 1 && points.first.latitude == 0)) return;

      final destination = points.last;
      final distanceVal = distance?.toDouble();
      final durationVal = duration?.toDouble();

      if (!mounted) return;
      setState(() {
        _deliveryRoutePolyline = points;
        _deliveryDistanceMeters = distanceVal;
        _deliveryEtaSeconds = durationVal;
        _mapCenter = destination;
        _mapMarkers['destination'] = MarkerData(
          position: destination,
          iconSize: 1.0,
        );
      });

      if (_mapController != null) {
        unawaited(
          _mapController!.animateCamera(
            CameraUpdate.newLatLng(points.first),
          ),
        );
      }
    } catch (e) {
      debugPrint('[DeliveryRoute] Failed to fetch route for order $orderId: $e');
    }
  }

  void _showCartBottomSheet() {
    Navigator.push(context, MaterialPageRoute<void>(builder: (_) => const CartScreen()));
  }

  List<Product> _getFilteredProducts() {
    // Server-side filtering is now used — `_allProducts` already reflects
    // the current `_searchQuery` + `_selectedCategory` from the backend.
    return _allProducts;
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: AppTheme.bgColor,
      body: SafeArea(
        child: Stack(
          children: [
            IndexedStack(
              index: _currentIndex,
              children: [
                _buildHomeTab(),
                _buildCatalogTab(),
                _buildWishlistTab(),
                _buildMapTab(),
                _buildOrdersTab(),
                _buildProfileTab(),
              ],
            ),
          ],
        ),
      ),
      bottomNavigationBar: _buildBottomNavbar(),
    );
  }

  Widget _buildHomeTab() {
    return RefreshIndicator(
      color: AppTheme.limeAccent,
      backgroundColor: AppTheme.blackAccent,
      onRefresh: () => _fetchCatalog(reset: true),
      child: SingleChildScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.all(24.0),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                        'Welcome, ${SessionRegistry.instance.fullName ?? 'User'}',
                        style: const TextStyle(fontSize: 16, color: Colors.grey),),
                    const SizedBox(height: 4),
                    Text('OMNIGO Customer',
                        style: const TextStyle(
                            fontSize: 14,
                            fontWeight: FontWeight.bold,
                            color: AppTheme.blackAccent,),),
                  ],
                ),
                Container(
                  padding: const EdgeInsets.all(10),
                  decoration: const BoxDecoration(
                      color: AppTheme.blackAccent, shape: BoxShape.circle,),
                  child: const ChatNavButton(
                    iconColor: Colors.white,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 30),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(24),
              decoration: BoxDecoration(
                color: AppTheme.limeAccent,
                borderRadius: BorderRadius.circular(30),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('Summer Sale',
                      style: TextStyle(
                          fontSize: 24,
                          fontWeight: FontWeight.w900,
                          color: AppTheme.blackAccent,),),
                  const SizedBox(height: 8),
                  const Text('Up to 50% off on premium items',
                      style:
                          TextStyle(fontSize: 14, color: AppTheme.blackAccent),),
                  const SizedBox(height: 20),
                  ElevatedButton(
                    onPressed: () =>
                        setState(() => _currentIndex = 1), // Go to catalog tab
                    style: ElevatedButton.styleFrom(
                      backgroundColor: AppTheme.blackAccent,
                      foregroundColor: Colors.white,
                      elevation: 0,
                    ),
                    child: const Text('Shop Now'),
                  ),
                ],
              ),
            ),
            const SizedBox(height: 30),
            const Text('Top Categories',
                style: TextStyle(
                    fontSize: 20,
                    fontWeight: FontWeight.bold,
                    color: AppTheme.blackAccent,),),
            const SizedBox(height: 16),
            SingleChildScrollView(
              scrollDirection: Axis.horizontal,
              child: Row(
                children:
                    ['Shoes', 'Apparel', 'Electronics', 'Accessories'].map((cat) {
                  return GestureDetector(
                    onTap: () {
                      setState(() {
                        _selectedCategory = cat;
                        _currentIndex = 1;
                      });
                    },
                    child: Container(
                      margin: const EdgeInsets.only(right: 16),
                      width: 100,
                      height: 100,
                      decoration: BoxDecoration(
                        color: Colors.white,
                        borderRadius: BorderRadius.circular(20),
                        boxShadow: [
                          BoxShadow(
                              color: Colors.black.withOpacity(0.02),
                              blurRadius: 10,
                              offset: const Offset(0, 5),),
                        ],
                      ),
                      child: Center(
                        child: Column(
                          mainAxisAlignment: MainAxisAlignment.center,
                          children: [
                            Icon(
                              cat == 'Shoes'
                                  ? Icons.snowshoeing
                                  : cat == 'Electronics'
                                      ? Icons.devices
                                      : Icons.category,
                              color: AppTheme.blackAccent,
                              size: 30,
                            ),
                            const SizedBox(height: 8),
                            Text(cat,
                                style: const TextStyle(
                                    fontWeight: FontWeight.bold,
                                    fontSize: 13,
                                    color: AppTheme.blackAccent,),),
                          ],
                        ),
                      ),
                    ),
                  );
                }).toList(),
              ),
            ),
            const SizedBox(height: 30),
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                const Text('Featured Products',
                    style: TextStyle(
                        fontSize: 20,
                        fontWeight: FontWeight.bold,
                        color: AppTheme.blackAccent,),),
                GestureDetector(
                  onTap: () => setState(() => _currentIndex = 1),
                  child: const Text('See All',
                      style: TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.bold,
                          color: Colors.blue,),),
                ),
              ],
            ),
            const SizedBox(height: 16),
            if (_allProducts.isNotEmpty)
              GridView.builder(
                shrinkWrap: true,
                physics: const NeverScrollableScrollPhysics(),
                itemCount: _allProducts.length > 8 ? 8 : _allProducts.length,
                gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                  crossAxisCount: 2,
                  crossAxisSpacing: 16,
                  mainAxisSpacing: 16,
                  childAspectRatio: 0.75,
                ),
                itemBuilder: (context, idx) {
                  return _buildProductCard(_allProducts[idx]);
                },
              )
            else if (_isLoadingCatalog)
              const Center(
                child: Padding(
                  padding: EdgeInsets.all(20.0),
                  child: CircularProgressIndicator(color: AppTheme.limeAccent),
                ),
              )
            else
              const Center(
                child: Padding(
                  padding: EdgeInsets.all(20.0),
                  child: Text('Featured products currently unavailable.',
                      style: TextStyle(color: Colors.grey),),
                ),
              ),
          ],
        ),
      ),
    );
  }

  Widget _buildCatalogTab() {
    final products = _getFilteredProducts();

    return RefreshIndicator(
      color: AppTheme.limeAccent,
      backgroundColor: AppTheme.blackAccent,
      onRefresh: () => _fetchCatalog(reset: true),
      child: SingleChildScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        controller: _scrollController,
        padding: const EdgeInsets.all(24.0),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [
              Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const Text('OMNIGO E-Shop',
                      style: TextStyle(fontSize: 16, color: Colors.grey),),
                  const SizedBox(height: 4),
                  Text(SessionRegistry.instance.fullName ?? 'User',
                      style: const TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.bold,
                          color: AppTheme.blackAccent,),),
                ],
              ),
              GestureDetector(
                onTap: _showCartBottomSheet,
                child: Stack(
                  alignment: Alignment.topRight,
                  children: [
                    Container(
                      padding: const EdgeInsets.all(10),
                      decoration: const BoxDecoration(
                          color: AppTheme.blackAccent, shape: BoxShape.circle,),
                      child: const Icon(Icons.shopping_cart_outlined,
                          color: Colors.white, size: 20,),
                    ),
                    Consumer<CartProvider>(
                      builder: (context, cart, child) {
                        if (cart.itemCount == 0) return const SizedBox.shrink();
                        return Container(
                          padding: const EdgeInsets.all(4),
                          decoration: const BoxDecoration(
                              color: Colors.redAccent, shape: BoxShape.circle,),
                          constraints:
                              const BoxConstraints(minWidth: 16, minHeight: 16),
                          child: Text(
                            '${cart.itemCount}',
                            style: const TextStyle(
                                color: Colors.white,
                                fontSize: 9,
                                fontWeight: FontWeight.bold,),
                            textAlign: TextAlign.center,
                          ),
                        );
                      },
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 24),
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            decoration: BoxDecoration(
              color: Colors.white,
              borderRadius: BorderRadius.circular(20),
              boxShadow: [
                BoxShadow(
                    color: Colors.black.withOpacity(0.02),
                    blurRadius: 10,
                    offset: const Offset(0, 5),),
              ],
            ),
            child: TextField(
              controller: _searchController,
              onChanged: _onSearchChanged,
              decoration: const InputDecoration(
                hintText: 'Search products and brands...',
                prefixIcon: Icon(Icons.search, color: Colors.grey),
                border: InputBorder.none,
                enabledBorder: InputBorder.none,
                focusedBorder: InputBorder.none,
              ),
            ),
          ),
          const SizedBox(height: 20),
          SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: Row(
              children: ['All', 'Shoes', 'Apparel', 'Electronics'].map((cat) {
                final isSelected = _selectedCategory == cat;
                return GestureDetector(
                  onTap: () => _onCategorySelected(cat),
                  child: Container(
                    margin: const EdgeInsets.only(right: 12),
                    padding: const EdgeInsets.symmetric(
                        horizontal: 20, vertical: 10,),
                    decoration: BoxDecoration(
                      color: isSelected ? AppTheme.blackAccent : Colors.white,
                      borderRadius: BorderRadius.circular(20),
                      border: Border.all(color: Colors.grey.shade200),
                    ),
                    child: Text(
                      cat,
                      style: TextStyle(
                        fontWeight: FontWeight.bold,
                        color: isSelected ? Colors.white : AppTheme.blackAccent,
                        fontSize: 13,
                      ),
                    ),
                  ),
                );
              }).toList(),
            ),
          ),
          const SizedBox(height: 24),
          Text(
            '$_selectedCategory Catalog (${products.length})',
            style: const TextStyle(
                fontSize: 20,
                fontWeight: FontWeight.bold,
                color: AppTheme.blackAccent,),
          ),
          const SizedBox(height: 16),
          if (products.isEmpty && !_isLoadingCatalog)
            const Center(
              child: Padding(
                padding: EdgeInsets.symmetric(vertical: 40.0),
                child: Text('No products found.',
                    style: TextStyle(color: Colors.grey),),
              ),
            )
          else
            GridView.builder(
              shrinkWrap: true,
              physics: const NeverScrollableScrollPhysics(),
              itemCount: products.length,
              gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(
                crossAxisCount: 2,
                crossAxisSpacing: 16,
                mainAxisSpacing: 16,
                childAspectRatio: 0.75,
              ),
              itemBuilder: (context, idx) {
                return _buildProductCard(products[idx]);
              },
            ),
          if (_isLoadingCatalog)
            const Center(
              child: Padding(
                padding: EdgeInsets.all(20),
                child: CircularProgressIndicator(color: AppTheme.limeAccent),
              ),
            ),
        ],
      ),
    ),
  );
}

  Widget _buildProductCard(Product p) {
    final String name = p.name.isNotEmpty ? p.name : 'Unknown';
    final double price = p.basePrice;
    final String storeId = p.storeTrackingId;
    final String prodId = p.productTrackingId.isNotEmpty ? p.productTrackingId : 'PROD-N/A';
    final bool isFavorited = _favoriteProductIds.contains(prodId);

    return GestureDetector(
      onTap: () {
        Navigator.push(
          context,
          MaterialPageRoute<void>(
            builder: (context) => ProductDetailsScreen(
              product: p,
              userTrackingId: widget.trackingId,
            ),
          ),
        );
      },
      child: Container(
        decoration: BoxDecoration(
          color: Colors.white,
          borderRadius: BorderRadius.circular(24),
          boxShadow: [
            BoxShadow(
                color: Colors.black.withOpacity(0.02),
                blurRadius: 10,
                offset: const Offset(0, 5),),
          ],
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: Stack(
                children: [
                  Container(
                    width: double.infinity,
                    decoration: const BoxDecoration(
                      color: AppTheme.softBlue,
                      borderRadius: BorderRadius.vertical(top: Radius.circular(24)),
                    ),
                    child: ClipRRect(
                      borderRadius: const BorderRadius.vertical(top: Radius.circular(24)),
                      child: (p.imageUrl != null && p.imageUrl!.isNotEmpty)
                          ? Image.network(
                              p.imageUrl!,
                              fit: BoxFit.cover,
                              errorBuilder: (context, error, stackTrace) =>
                                  const Center(child: Icon(Icons.shopping_bag_outlined, size: 40, color: AppTheme.blackAccent)),
                            )
                          : const Center(child: Icon(Icons.shopping_bag_outlined, size: 40, color: AppTheme.blackAccent)),
                    ),
                  ),
                  // Favorite heart toggle
                  Positioned(
                    top: 8,
                    right: 8,
                    child: GestureDetector(
                      onTap: () => _toggleFavorite(prodId),
                      child: Container(
                        padding: const EdgeInsets.all(6),
                        decoration: BoxDecoration(
                          color: Colors.white.withOpacity(0.9),
                          shape: BoxShape.circle,
                        ),
                        child: Icon(
                          isFavorited ? Icons.favorite : Icons.favorite_border_rounded,
                          color: isFavorited ? Colors.redAccent : Colors.grey,
                          size: 18,
                        ),
                      ),
                    ),
                  ),
                ],
              ),
            ),
            Padding(
              padding: const EdgeInsets.all(12.0),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(name,
                      style: const TextStyle(
                          fontWeight: FontWeight.bold,
                          color: AppTheme.blackAccent,
                          fontSize: 15,),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,),
                  const SizedBox(height: 4),
                  Text(storeId,
                      style: const TextStyle(color: Colors.grey, fontSize: 11),),
                  const SizedBox(height: 8),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      Text('PKR ${price.toStringAsFixed(2)}',
                          style: const TextStyle(
                              fontWeight: FontWeight.w800,
                              color: AppTheme.blackAccent,),),
                      GestureDetector(
                        onTap: () {
                          context.read<CartProvider>().addItem(p);
                          ScaffoldMessenger.of(context).showSnackBar(
                            SnackBar(
                              content: Text('Added $name to cart!'),
                              backgroundColor: Colors.green,
                              behavior: SnackBarBehavior.floating,
                              duration: const Duration(milliseconds: 800),
                            ),
                          );
                        },
                        child: Container(
                          padding: const EdgeInsets.all(8),
                          decoration: const BoxDecoration(
                              color: AppTheme.blackAccent,
                              shape: BoxShape.circle,),
                          child: const Icon(Icons.add_shopping_cart_outlined,
                              color: AppTheme.limeAccent, size: 14,),
                        ),
                      ),
                    ],
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildMapTab() {
    return Stack(
      children: [
        // ── Live Rider Tracking Layer ────────────────────────────
        // Rebuilds ONLY when rider coordinates change, leaving the
        // base map tiles untouched (no global repaint).
        ValueListenableBuilder<Map<String, LatLng>>(
          valueListenable: _riderMarkers,
          builder: (context, riderCoords, _) {
            // Merge the live rider positions into the static marker
            // map keyed by "rider:<id>" so the MapLibre widget can
            // reconcile symbols efficiently via didUpdateWidget.
            final merged = Map<String, MarkerData>.from(_mapMarkers);
            riderCoords.forEach((id, pos) {
              merged['rider:$id'] = MarkerData(
                position: pos,
                iconSize: 1.0,
              );
            });

            return MapLibreMapWidget(
              initialCenter: _mapCenter,
              initialZoom: 13.0,
              markers: merged,
              polylines: _deliveryRoutePolyline.isNotEmpty
                  ? [_deliveryRoutePolyline]
                  : (_rideRoutePolyline.isNotEmpty
                      ? [_rideRoutePolyline]
                      : const []),
              onMapCreated: (controller) {
                _mapController = controller;
                if (_deliveryRoutePolyline.isNotEmpty && _deliveryRoutePolyline.first.latitude != 0) {
                  controller.animateCamera(
                    CameraUpdate.newLatLng(_deliveryRoutePolyline.first),
                  );
                }
              },
            );
          },
        ),
        Positioned(
          top: 0,
          left: 0,
          right: 0,
          child: SafeArea(
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 16),
                decoration: BoxDecoration(
                  color: Colors.white,
                  borderRadius: BorderRadius.circular(30),
                  boxShadow: const [
                    BoxShadow(color: Colors.black12, blurRadius: 10),
                  ],
                ),
                child: Row(
                  children: [
                    const Icon(Icons.location_on, color: AppTheme.blackAccent),
                    const SizedBox(width: 10),
                    Expanded(
                      child: TextField(
                    controller: _mapSearchController,
                    textInputAction: TextInputAction.search,
                    onSubmitted: (val) => _searchMap(val),
                    decoration: const InputDecoration(
                      hintText: 'Search city (Lahore, Karachi, London...)',
                      border: InputBorder.none,
                      enabledBorder: InputBorder.none,
                      focusedBorder: InputBorder.none,
                    ),
                  ),
                ),
                IconButton(
                  icon: _isGeocoding
                      ? const SizedBox(
                          width: 20,
                          height: 20,
                          child: CircularProgressIndicator(
                              strokeWidth: 2, color: AppTheme.blackAccent,),
                        )
                      : const Icon(Icons.search, color: AppTheme.blackAccent),
                  onPressed: _isGeocoding
                      ? null
                      : () => _searchMap(_mapSearchController.text),
                ),
              ],
            ),
          ),
        ),
      ),
      ),
        // H4: Delivery ETA + distance banner (shown when rider is en route)
        if (_deliveryEtaSeconds != null && _activeDeliveryOrderId != null)
          Positioned(
            bottom: 48,
            left: 0,
            right: 0,
            child: SafeArea(
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
                child: Container(
                  padding: const EdgeInsets.all(12),
                  decoration: BoxDecoration(
                    color: const Color(0xFFCAFF33),
                    borderRadius: BorderRadius.circular(16),
                  ),
                  child: Row(
                    children: [
                      const Icon(Icons.local_shipping_outlined,
                          color: Colors.black, size: 18,),
                      const SizedBox(width: 10),
                      Expanded(
                        child: Text(
                          'Estimated arrival: ~${(_deliveryEtaSeconds! / 60).round()} min'
                          '${_deliveryDistanceMeters != null ? ' • ${(_deliveryDistanceMeters! / 1000).toStringAsFixed(1)} km away' : ''}',
                          style: const TextStyle(
                            color: Colors.black,
                            fontSize: 12,
                            fontWeight: FontWeight.bold,
                          ),
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            ),
          ),
        // Live rider tracking status banner (shown when no active ride search)
        if (_rideSearchStatus == 'idle')
          Positioned(
            bottom: 0,
            left: 0,
            right: 0,
            child: SafeArea(
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 8),
                child: ValueListenableBuilder<Map<String, LatLng>>(
                  valueListenable: _riderMarkers,
                  builder: (context, riderCoords, _) {
                    if (riderCoords.isEmpty) return const SizedBox.shrink();
                    return Container(
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: Colors.black.withOpacity(0.85),
                        borderRadius: BorderRadius.circular(16),
                      ),
                      child: Row(
                        children: [
                          const Icon(Icons.radar_rounded,
                              color: Color(0xFFCAFF33), size: 18,),
                          const SizedBox(width: 10),
                          Expanded(
                            child: Text(
                              'Live rider tracking: ${riderCoords.length} ${riderCoords.length == 1 ? "rider" : "riders"} on the move',
                              style: const TextStyle(
                                  color: Colors.white,
                                  fontSize: 12,
                                  fontWeight: FontWeight.bold,),
                            ),
                          ),
                        ],
                      ),
                    );
                  },
                ),
              ),
            ),
          ),
        // Active ride radar and driver discovery panel
        if (_rideSearchStatus != 'idle')
          Positioned(
            bottom: 12,
            left: 16,
            right: 16,
            child: SafeArea(
              child: _buildRideRadarPanel(),
            ),
          ),
        if (_isEstimatingRide)
          Positioned.fill(
            child: Container(
              color: Colors.black.withOpacity(0.5),
              child: const Center(
                child: CircularProgressIndicator(
                  color: Color(0xFFCAFF33),
                ),
              ),
            ),
          ),
      ],
    );
  }

  Widget _buildOrdersTab() {
    return MyOrdersScreen(customerTrackingId: widget.trackingId);
  }

  Widget _buildProfileTab() {
    return SingleChildScrollView(
      padding: const EdgeInsets.all(24.0),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text('Customer Profile',
              style: TextStyle(
                  fontSize: 26,
                  fontWeight: FontWeight.bold,
                  color: AppTheme.blackAccent,),),
          const SizedBox(height: 24),
          Container(
            padding: const EdgeInsets.all(24),
            decoration: BoxDecoration(
              color: AppTheme.softPink,
              borderRadius: BorderRadius.circular(30),
            ),
            child: Row(
              children: [
                const CircleAvatar(
                  radius: 35,
                  backgroundColor: AppTheme.blackAccent,
                  child: Icon(Icons.person, size: 40, color: Colors.white),
                ),
                const SizedBox(width: 20),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(SessionRegistry.instance.fullName ?? 'Unknown User',
                          style: const TextStyle(
                              fontSize: 20,
                              fontWeight: FontWeight.bold,
                              color: AppTheme.blackAccent,),),
                      const SizedBox(height: 4),
                      Text(widget.trackingId,
                          style: const TextStyle(
                              fontSize: 14,
                              fontWeight: FontWeight.bold,
                              color: Colors.grey,),),
                    ],
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(height: 30),
          const Text('Account Information',
              style: TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.bold,
                  color: AppTheme.blackAccent,),),
          const SizedBox(height: 16),
          _buildEmailRow(
            SessionRegistry.instance.email ?? 'Not provided',
            SessionRegistry.instance.isVerified,
          ),
          _buildInfoRow(
              'Phone Number',
              SessionRegistry.instance.phone?.isNotEmpty == true
                  ? SessionRegistry.instance.phone!
                  : 'Not provided',
              Icons.phone_outlined,),
          _buildInfoRow(
              'Default Address',
              SessionRegistry.instance.address?.isNotEmpty == true
                  ? SessionRegistry.instance.address!
                  : 'Not provided',
              Icons.home_outlined,),
          const SizedBox(height: 30),
          const Text('Payment Methods',
              style: TextStyle(
                  fontSize: 18,
                  fontWeight: FontWeight.bold,
                  color: AppTheme.blackAccent,),),
          const SizedBox(height: 16),
          // Payment method cards — wired to Stripe integration status.
          // When STRIPE_API_KEY is set on the backend, the checkout flow
          // creates a real Payment Intent. Until then, show a clear
          // "Add Payment Method" call-to-action.
          _buildPaymentCard(
            'Stripe Credit / Debit',
            'Tap to manage saved cards',
            Colors.blue.shade100,
            Icons.credit_card,
            onTap: () {
              Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (_) => const CustomerSavedCardsScreen(),
                ),
              );
            },
          ),
          _buildPaymentCard(
            'JazzCash / EasyPaisa',
            'Tap to load your mobile wallet',
            Colors.amber.shade100,
            Icons.account_balance_wallet_outlined,
            onTap: () {
              Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (_) => CustomerWalletScreen(
                    trackingId: widget.trackingId,
                  ),
                ),
              );
            },
          ),
          const SizedBox(height: 30),
          // Edit Profile button
          ElevatedButton.icon(
            onPressed: () async {
              final result = await Navigator.push<bool>(
                context,
                MaterialPageRoute(builder: (_) => const EditProfileScreen()),
              );
              // Refresh the profile tab display after an edit.
              if (result == true && mounted) setState(() {});
            },
            icon: const Icon(Icons.edit_outlined, color: Colors.white),
            label: const Text('Edit Profile', style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16)),
            style: ElevatedButton.styleFrom(
              backgroundColor: AppTheme.blackAccent,
              foregroundColor: Colors.white,
              minimumSize: const Size(double.infinity, 54),
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
            ),
          ),
          const SizedBox(height: 16),
          OutlinedButton.icon(
            onPressed: () {
              Navigator.push(
                context,
                MaterialPageRoute<void>(
                  builder: (_) => const SecuritySettingsScreen(),
                ),
              );
            },
            icon: const Icon(Icons.shield_outlined, color: AppTheme.blackAccent),
            label: const Text(
              'Security Settings',
              style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16, color: AppTheme.blackAccent),
            ),
            style: OutlinedButton.styleFrom(
              minimumSize: const Size(double.infinity, 54),
              shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16)),
              side: const BorderSide(color: AppTheme.blackAccent),
            ),
          ),
          const SizedBox(height: 16),
          ElevatedButton(
            onPressed: () async {
              // Clear ALL session data (memory + SharedPreferences) before navigating
              try {
                await SessionRegistry.instance.logout();
              } finally {
                if (mounted) {
                  unawaited(Navigator.pushNamedAndRemoveUntil(context, '/', (route) => false));
                }
              }
            },
            style: ElevatedButton.styleFrom(
              backgroundColor: Colors.redAccent,
              foregroundColor: Colors.white,
              minimumSize: const Size(double.infinity, 54),
            ),
            child: const Text('Log Out',
                style: TextStyle(fontWeight: FontWeight.bold, fontSize: 16),),
          ),
          const SizedBox(height: 20),
        ],
      ),
    );
  }

  Widget _buildInfoRow(String title, String value, IconData icon) {
    return Container(
      margin: const EdgeInsets.only(bottom: 12),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: Colors.grey.shade100),
      ),
      child: Row(
        children: [
          Icon(icon, color: Colors.grey),
          const SizedBox(width: 16),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title,
                    style: const TextStyle(fontSize: 12, color: Colors.grey),),
                const SizedBox(height: 2),
                Text(value,
                    style: const TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.bold,
                        color: AppTheme.blackAccent,),),
              ],
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildEmailRow(String email, bool isVerified) {
    return Container(
      margin: const EdgeInsets.only(bottom: 12),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: Colors.white,
        borderRadius: BorderRadius.circular(20),
        border: Border.all(color: Colors.grey.shade100),
      ),
      child: Row(
        children: [
          Icon(
            isVerified ? Icons.check_circle : Icons.email_outlined,
            color: isVerified ? Colors.green : Colors.grey,
          ),
          const SizedBox(width: 16),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const Text('Email Address',
                    style: TextStyle(fontSize: 12, color: Colors.grey),),
                const SizedBox(height: 2),
                Text(email,
                    style: const TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.bold,
                        color: AppTheme.blackAccent,),),
                if (!isVerified)
                  const Text('Not verified',
                      style: TextStyle(fontSize: 11, color: Colors.orange),),
              ],
            ),
          ),
          if (!isVerified)
            TextButton(
              onPressed: () => _sendVerificationEmail(email),
              style: TextButton.styleFrom(
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                minimumSize: Size.zero,
                tapTargetSize: MaterialTapTargetSize.shrinkWrap,
              ),
              child: const Text(
                'Verify',
                style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold),
              ),
            ),
        ],
      ),
    );
  }

  Future<void> _sendVerificationEmail(String email) async {
    try {
      await sl<ApiClient>().post(ApiEndpoints.authSendVerificationEmail(), {});
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(
            content: Text('Verification email sent! Check your inbox.'),
            backgroundColor: Colors.green,
            behavior: SnackBarBehavior.floating,
          ),
        );
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Failed to send verification email: $e'),
            backgroundColor: Colors.redAccent,
            behavior: SnackBarBehavior.floating,
          ),
        );
      }
    }
  }

  Widget _buildPaymentCard(
      String gateway, String detail, Color color, IconData icon,
      {VoidCallback? onTap,}) {
    return GestureDetector(
      onTap: onTap,
      child: Container(
        margin: const EdgeInsets.only(bottom: 12),
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: color,
          borderRadius: BorderRadius.circular(20),
        ),
        child: Row(
          children: [
            Icon(icon, color: AppTheme.blackAccent),
            const SizedBox(width: 16),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(gateway,
                      style: const TextStyle(
                          fontSize: 14,
                          fontWeight: FontWeight.bold,
                          color: AppTheme.blackAccent,),),
                  const SizedBox(height: 2),
                  Text(detail,
                      style: const TextStyle(
                          fontSize: 12, color: AppTheme.blackAccent,),),
                ],
              ),
            ),
            Icon(onTap != null ? Icons.add_circle_outline : Icons.lock_outline,
                color: AppTheme.blackAccent.withOpacity(0.5),),
          ],
        ),
      ),
    );
  }

  Widget _buildWishlistTab() {
    return WishlistScreen(
      key: _wishlistKey,
      customerTrackingId: widget.trackingId,
      onNavigateToCatalog: (index) => setState(() => _currentIndex = index),
    );
  }

  Widget _buildBottomNavbar() {
    return Container(
      margin: const EdgeInsets.symmetric(horizontal: 30, vertical: 16),
      padding: const EdgeInsets.symmetric(vertical: 14, horizontal: 20),
      decoration: BoxDecoration(
        color: AppTheme.blackAccent.withOpacity(0.95),
        borderRadius: BorderRadius.circular(40),
        boxShadow: const [
          BoxShadow(color: Colors.black26, blurRadius: 10, offset: Offset(0, 5)),
        ],
      ),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceAround,
        children: [
          _buildNavItem(Icons.home_filled, 0),
          _buildNavItem(Icons.search, 1),
          _buildNavItem(Icons.favorite_outline_rounded, 2),
          _buildNavItem(Icons.map_outlined, 3),
          _buildNavItem(Icons.local_shipping_outlined, 4),
          _buildNavItem(Icons.person_outline, 5),
        ],
      ),
    );
  }

  Widget _buildNavItem(IconData icon, int index) {
    final isSelected = _currentIndex == index;
    return GestureDetector(
      onTap: () {
        if (index == 4) {
          Navigator.push(
            context,
            MaterialPageRoute<void>(
              builder: (_) => MyOrdersScreen(customerTrackingId: widget.trackingId),
            ),
          );
        } else {
          setState(() {
            _currentIndex = index;
          });
          // Refresh wishlist when tab is selected
          if (index == 2) {
            _wishlistKey.currentState?.fetchWishlist();
          }
        }
      },
      child: Icon(
        icon,
        color: isSelected ? AppTheme.limeAccent : Colors.white.withOpacity(0.5),
        size: 28,
      ),
    );
  }
}
