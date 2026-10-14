import 'dart:async';
import 'package:flutter/material.dart';
import 'package:audioplayers/audioplayers.dart';

class NotificationAlertDialog extends StatefulWidget {

  const NotificationAlertDialog({
    super.key,
    required this.gigData,
    required this.onAccept,
    required this.onDecline,
  });
  final Map<String, dynamic> gigData;
  final VoidCallback onAccept;
  final VoidCallback onDecline;

  @override
  NotificationAlertDialogState createState() => NotificationAlertDialogState();
}

class NotificationAlertDialogState extends State<NotificationAlertDialog> {
  int _secondsLeft = 15;
  Timer? _timer;
  final AudioPlayer _audioPlayer = AudioPlayer();

  @override
  void initState() {
    super.initState();
    _startTimer();
    _playSound();
  }

  void _startTimer() {
    _timer = Timer.periodic(const Duration(seconds: 1), (timer) {
      if (!mounted) { timer.cancel(); return; }
      if (_secondsLeft > 0) {
        setState(() {
          _secondsLeft--;
        });
      } else {
        _timer?.cancel();
        widget.onDecline(); // Auto decline/close on timeout
      }
    });
  }

  Future<void> _playSound() async {
    try {
      await _audioPlayer.setReleaseMode(ReleaseMode.loop);
      await _audioPlayer.play(AssetSource('sounds/ringtone.mp3'));
    } catch (e) {
      debugPrint('Audio playback error: $e');
    }
  }

  @override
  void dispose() {
    _timer?.cancel();
    _audioPlayer.stop();
    _audioPlayer.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Dialog(
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20)),
      elevation: 10,
      backgroundColor: Colors.white,
      child: Padding(
        padding: const EdgeInsets.all(20.0),
        child: Builder(
          builder: (context) {
            final isRide = (widget.gigData['action']?.toString().toUpperCase() == 'RIDE_BROADCAST') ||
                (widget.gigData['vehicle_type'] != null) ||
                (widget.gigData['tracking_id']?.toString().startsWith('RIDE-') ?? false);

            final vehicleType = widget.gigData['vehicle_type']?.toString().toLowerCase() ?? 'car';
            IconData vehicleIcon = Icons.directions_car;
            if (vehicleType.contains('bike') || vehicleType.contains('motorcycle')) {
              vehicleIcon = Icons.two_wheeler;
            } else if (vehicleType.contains('rickshaw') || vehicleType.contains('auto')) {
              vehicleIcon = Icons.electric_rickshaw;
            }

            final fare = widget.gigData['fare_amount'] ?? widget.gigData['rider_earning'] ?? widget.gigData['delivery_fee'] ?? 0;
            final paymentMethod = widget.gigData['payment_method']?.toString().toUpperCase() ?? 'CASH';

            return Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                Icon(isRide ? vehicleIcon : Icons.delivery_dining, size: 60, color: isRide ? Colors.orangeAccent : Colors.blueAccent),
                const SizedBox(height: 15),
                Text(
                  isRide ? "New Ride Request!" : "New Delivery Request!",
                  style: const TextStyle(fontSize: 22, fontWeight: FontWeight.bold),
                  textAlign: TextAlign.center,
                ),
                const SizedBox(height: 10),
                Text(
                  isRide ? "Fare: PKR $fare" : "Earning: PKR $fare",
                  style: const TextStyle(fontSize: 18, color: Colors.green, fontWeight: FontWeight.w600),
                ),
                if (isRide) ...[
                  const SizedBox(height: 6),
                  Row(
                    mainAxisAlignment: MainAxisAlignment.center,
                    children: [
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                        decoration: BoxDecoration(color: Colors.grey.shade200, borderRadius: BorderRadius.circular(6)),
                        child: Text(
                          vehicleType.toUpperCase(),
                          style: const TextStyle(fontSize: 12, fontWeight: FontWeight.bold, color: Colors.black87),
                        ),
                      ),
                      const SizedBox(width: 8),
                      Container(
                        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                        decoration: BoxDecoration(color: Colors.green.shade50, borderRadius: BorderRadius.circular(6)),
                        child: Text(
                          'PAY: $paymentMethod',
                          style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold, color: Colors.green.shade800),
                        ),
                      ),
                    ],
                  ),
                ],
                if (!isRide && widget.gigData['delivery_fee'] != null) ...[
                  const SizedBox(height: 6),
                  Text(
                    "Delivery Fee: PKR ${widget.gigData['delivery_fee']}",
                    style: TextStyle(fontSize: 14, color: Colors.grey.shade600, fontWeight: FontWeight.w500),
                  ),
                ],
                if (!isRide && widget.gigData['items_summary'] != null && (widget.gigData['items_summary'] as String).isNotEmpty) ...[
                  const SizedBox(height: 12),
                  Container(
                    padding: const EdgeInsets.all(10),
                    decoration: BoxDecoration(
                      color: Colors.blue.shade50,
                      borderRadius: BorderRadius.circular(10),
                      border: Border.all(color: Colors.blue.shade100),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          children: [
                            Icon(Icons.shopping_bag_outlined, size: 16, color: Colors.blue.shade700),
                            const SizedBox(width: 6),
                            Text('Items', style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: Colors.blue.shade700)),
                          ],
                        ),
                        const SizedBox(height: 4),
                        Text(
                          widget.gigData['items_summary']?.toString() ?? '',
                          style: TextStyle(fontSize: 13, color: Colors.blue.shade900),
                          maxLines: 3,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ],
                    ),
                  ),
                ],
                if (widget.gigData['customer_name'] != null && widget.gigData['customer_name'].toString().isNotEmpty) ...[
                  const SizedBox(height: 10),
                  Row(
                    children: [
                      Icon(Icons.person_outline, size: 16, color: Colors.grey.shade600),
                      const SizedBox(width: 6),
                      Expanded(
                        child: Text(
                          isRide ? 'Passenger: ${widget.gigData['customer_name']}' : 'Deliver to: ${widget.gigData['customer_name']}',
                          style: TextStyle(fontSize: 13, color: Colors.grey.shade700, fontWeight: FontWeight.w500),
                        ),
                      ),
                    ],
                  ),
                ],
                if (widget.gigData['customer_address'] != null && widget.gigData['customer_address'].toString().isNotEmpty) ...[
                  const SizedBox(height: 6),
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Icon(Icons.location_on_outlined, size: 16, color: Colors.grey.shade600),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      widget.gigData['customer_address']?.toString() ?? '',
                      style: TextStyle(fontSize: 12, color: Colors.grey.shade600),
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ),
            ],
            const SizedBox(height: 20),
            Text(
              "00:${_secondsLeft.toString().padLeft(2, '0')}",
              style: const TextStyle(fontSize: 36, fontWeight: FontWeight.bold, color: Colors.redAccent),
            ),
            const SizedBox(height: 30),
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceEvenly,
              children: [
                OutlinedButton(
                  onPressed: () {
                    _timer?.cancel();
                    _audioPlayer.stop();
                    widget.onDecline();
                  },
                  style: OutlinedButton.styleFrom(
                    foregroundColor: Colors.grey, side: const BorderSide(color: Colors.grey),
                    padding: const EdgeInsets.symmetric(horizontal: 20, vertical: 12),
                  ),
                  child: const Text("Decline", style: TextStyle(fontSize: 16)),
                ),
                ElevatedButton(
                  onPressed: () {
                    _timer?.cancel();
                    _audioPlayer.stop();
                    widget.onAccept();
                  },
                  style: ElevatedButton.styleFrom(
                    backgroundColor: Colors.blueAccent,
                    foregroundColor: Colors.white,
                    padding: const EdgeInsets.symmetric(horizontal: 30, vertical: 12),
                  ),
                  child: const Text("ACCEPT", style: TextStyle(fontSize: 18, fontWeight: FontWeight.bold)),
                ),
              ],
            ),
          ],
        );
      },
    ),
  ),
);
  }
}
