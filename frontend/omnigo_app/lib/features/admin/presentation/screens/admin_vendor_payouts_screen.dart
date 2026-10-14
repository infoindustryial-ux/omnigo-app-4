import 'dart:async';
import 'dart:io';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:path_provider/path_provider.dart';
import '../../../../core/network/api_endpoints.dart';
import '../../../../core/network/api_client.dart';
import '../../../../core/di/service_locator.dart';

class AdminVendorPayoutsScreen extends StatefulWidget {
  const AdminVendorPayoutsScreen({super.key});

  @override
  State<AdminVendorPayoutsScreen> createState() => _AdminVendorPayoutsScreenState();
}

class _AdminVendorPayoutsScreenState extends State<AdminVendorPayoutsScreen>
    with SingleTickerProviderStateMixin {
  late TabController _tabController;

  List<dynamic> _vendorPayouts = [];
  List<dynamic> _riderPayouts = [];
  bool _isLoading = true;
  bool _isExporting = false;
  String _selectedStatus = 'all';

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 2, vsync: this);
    _tabController.addListener(() {
      if (!_tabController.indexIsChanging) {
        _fetchPayouts();
      }
    });
    _fetchPayouts();
  }

  @override
  void dispose() {
    _tabController.dispose();
    super.dispose();
  }

  Future<void> _fetchPayouts() async {
    setState(() => _isLoading = true);
    final api = sl<ApiClient>();

    try {
      if (_tabController.index == 0) {
        final data = await api.get(
          ApiEndpoints.adminVendorPayouts(status: _selectedStatus),
        );
        if (data is Map<String, dynamic> && mounted) {
          setState(() {
            _vendorPayouts = data['payouts'] as List<dynamic>? ?? [];
            _isLoading = false;
          });
        }
      } else {
        final data = await api.get(
          ApiEndpoints.adminRiderPayouts(status: _selectedStatus),
        );
        if (data is Map<String, dynamic> && mounted) {
          setState(() {
            _riderPayouts = data['payouts'] as List<dynamic>? ?? [];
            _isLoading = false;
          });
        }
      }
    } catch (e) {
      if (mounted) {
        setState(() => _isLoading = false);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error loading payouts: $e'), backgroundColor: Colors.red),
        );
      }
    }
  }

  Future<void> _approvePayout(String id, bool isVendor) async {
    try {
      final endpoint = isVendor
          ? ApiEndpoints.adminVendorPayoutApprove(id)
          : ApiEndpoints.adminRiderPayoutApprove(id);
      await sl<ApiClient>().post(endpoint, {});
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Payout approved'), backgroundColor: Colors.green),
        );
        await _fetchPayouts();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Approval failed: $e'), backgroundColor: Colors.red),
        );
      }
    }
  }

  Future<void> _rejectPayout(String id, bool isVendor) async {
    final reasonController = TextEditingController(text: 'Rejected by admin');
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Reject Payout Request'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('Funds will be automatically refunded to the user wallet.'),
            const SizedBox(height: 12),
            TextField(
              controller: reasonController,
              decoration: const InputDecoration(
                labelText: 'Rejection Reason',
                border: OutlineInputBorder(),
              ),
            ),
          ],
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          ElevatedButton(
            style: ElevatedButton.styleFrom(backgroundColor: Colors.red),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Confirm Reject', style: TextStyle(color: Colors.white)),
          ),
        ],
      ),
    );

    if (confirmed != true) return;

    try {
      final endpoint = isVendor
          ? ApiEndpoints.adminVendorPayoutReject(id)
          : ApiEndpoints.adminRiderPayoutReject(id);
      await sl<ApiClient>().post(endpoint, {'reason': reasonController.text.trim()});
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          const SnackBar(content: Text('Payout rejected and wallet refunded'), backgroundColor: Colors.orange),
        );
        await _fetchPayouts();
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Rejection failed: $e'), backgroundColor: Colors.red),
        );
      }
    }
  }

  Future<void> _disburseSafepay(String id, bool isVendor) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Execute Safepay Raastwire Payout'),
        content: const Text(
          'This will trigger an INSTANT automated bank transfer to the beneficiary via Safepay Raastwire API.\n\nContinue?',
        ),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          ElevatedButton(
            style: ElevatedButton.styleFrom(backgroundColor: Colors.deepPurple),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Disburse Now ⚡', style: TextStyle(color: Colors.white)),
          ),
        ],
      ),
    );

    if (confirmed != true) return;

    setState(() => _isLoading = true);
    try {
      final endpoint = isVendor
          ? ApiEndpoints.adminVendorPayoutDisburseSafepay(id)
          : ApiEndpoints.adminRiderPayoutDisburseSafepay(id);
      final res = await sl<ApiClient>().post(endpoint, {});
      if (mounted) {
        final trace = (res is Map<String, dynamic>) ? res['trace_reference'] : '';
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(
            content: Text('Disbursement successful! Trace: $trace'),
            backgroundColor: Colors.green,
          ),
        );
        await _fetchPayouts();
      }
    } catch (e) {
      if (mounted) {
        setState(() => _isLoading = false);
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Safepay transfer failed: $e'), backgroundColor: Colors.red),
        );
      }
    }
  }

  Future<void> _export1IBFTCSV() async {
    setState(() => _isExporting = true);
    try {
      final type = _tabController.index == 0 ? 'vendor' : 'rider';
      final response = await sl<ApiClient>().get('${ApiEndpoints.adminExport1ibft()}?type=$type');
      if (mounted) {
        String csv = '';
        if (response is String) {
          csv = response;
        } else if (response is Map<String, dynamic>) {
          csv = response['csv']?.toString() ?? response['data']?.toString() ?? '';
        }
        if (csv.isNotEmpty) {
          final directory = await getApplicationDocumentsDirectory();
          final timestamp = DateTime.now().millisecondsSinceEpoch;
          final file = File('${directory.path}/1ibft_${type}_payouts_$timestamp.csv');
          await file.writeAsString(csv);
          if (mounted) {
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(
                content: Text('Saved 1IBFT CSV: ${file.path.split('/').last}'),
                backgroundColor: Colors.green,
              ),
            );
          }
        }
      }
    } catch (e) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Export failed: $e'), backgroundColor: Colors.red),
        );
      }
    } finally {
      if (mounted) setState(() => _isExporting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.grey[100],
      appBar: AppBar(
        title: const Text('Payouts Management', style: TextStyle(fontWeight: FontWeight.bold, color: Colors.white)),
        backgroundColor: Colors.black87,
        actions: [
          IconButton(
            icon: _isExporting
                ? const SizedBox(width: 18, height: 18, child: CircularProgressIndicator(color: Colors.white, strokeWidth: 2))
                : const Icon(Icons.file_download, color: Colors.white),
            tooltip: 'Export 1LINK/1IBFT CSV',
            onPressed: _isExporting ? null : _export1IBFTCSV,
          ),
          IconButton(
            icon: const Icon(Icons.refresh, color: Colors.white),
            tooltip: 'Refresh',
            onPressed: _fetchPayouts,
          ),
        ],
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(96),
          child: Column(
            children: [
              TabBar(
                controller: _tabController,
                indicatorColor: Colors.deepOrangeAccent,
                labelColor: Colors.white,
                unselectedLabelColor: Colors.grey[400],
                tabs: const [
                  Tab(icon: Icon(Icons.storefront), text: 'Vendor Payouts'),
                  Tab(icon: Icon(Icons.two_wheeler), text: 'Rider Payouts'),
                ],
              ),
              SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 6),
                child: Row(
                  children: [
                    _filterChip('All', 'all'),
                    const SizedBox(width: 6),
                    _filterChip('Pending', 'pending'),
                    const SizedBox(width: 6),
                    _filterChip('Scheduled', 'scheduled'),
                    const SizedBox(width: 6),
                    _filterChip('Approved', 'approved'),
                    const SizedBox(width: 6),
                    _filterChip('Paid / Completed', 'completed'),
                    const SizedBox(width: 6),
                    _filterChip('Failed / Rejected', 'rejected'),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
      body: TabBarView(
        controller: _tabController,
        children: [
          _buildPayoutsList(_vendorPayouts, isVendor: true),
          _buildPayoutsList(_riderPayouts, isVendor: false),
        ],
      ),
    );
  }

  Widget _filterChip(String label, String value) {
    final isSelected = _selectedStatus == value;
    return FilterChip(
      label: Text(label, style: TextStyle(fontSize: 12, color: isSelected ? Colors.white : Colors.grey.shade800)),
      selected: isSelected,
      selectedColor: Colors.deepOrangeAccent,
      backgroundColor: Colors.grey.shade800,
      onSelected: (_) {
        setState(() => _selectedStatus = value);
        _fetchPayouts();
      },
    );
  }

  Widget _buildPayoutsList(List<dynamic> payouts, {required bool isVendor}) {
    if (_isLoading) {
      return const Center(child: CircularProgressIndicator(color: Colors.black87));
    }
    if (payouts.isEmpty) {
      return Center(
        child: Column(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(isVendor ? Icons.storefront_outlined : Icons.two_wheeler_outlined, size: 64, color: Colors.grey[400]),
            const SizedBox(height: 12),
            Text(
              'No ${isVendor ? 'vendor' : 'rider'} payout requests found (${_selectedStatus.toUpperCase()})',
              style: TextStyle(color: Colors.grey[600], fontSize: 14),
            ),
          ],
        ),
      );
    }

    return RefreshIndicator(
      onRefresh: _fetchPayouts,
      child: ListView.builder(
        padding: const EdgeInsets.all(12),
        itemCount: payouts.length,
        itemBuilder: (context, index) => _buildPayoutCard(payouts[index] as Map<String, dynamic>, isVendor: isVendor),
      ),
    );
  }

  Widget _buildPayoutCard(Map<String, dynamic> payout, {required bool isVendor}) {
    final id = payout['id']?.toString() ?? '';
    final trackingId = isVendor
        ? (payout['vendor_tracking_id']?.toString() ?? '')
        : (payout['rider_tracking_id']?.toString() ?? '');
    final name = isVendor
        ? (payout['vendor_name']?.toString() ?? payout['beneficiary_name']?.toString() ?? 'Vendor')
        : (payout['rider_name']?.toString() ?? payout['account_title']?.toString() ?? 'Rider');
    final amount = (payout['amount'] as num?)?.toDouble() ?? 0.0;
    final feePaisa = (payout['payout_fee_paisa'] as num?)?.toInt() ?? 0;
    final feeRupees = feePaisa / 100.0;
    final netRupees = amount - feeRupees;

    final iban = isVendor
        ? (payout['beneficiary_iban']?.toString() ?? '')
        : (payout['beneficiary_iban']?.toString() ?? payout['account_number']?.toString() ?? '');
    final bankName = payout['bank_name']?.toString() ?? 'Bank';
    final status = payout['status']?.toString() ?? 'pending';
    final gateway = payout['gateway']?.toString() ?? 'safepay';
    final trace = payout['trace_reference']?.toString() ?? '';
    final failureReason = payout['failure_reason']?.toString() ?? '';
    final createdAt = payout['created_at']?.toString() ?? '';

    final statusColor = (status == 'pending' || status == 'scheduled')
        ? Colors.orange
        : (status == 'approved')
            ? Colors.blue
            : (status == 'paid' || status == 'completed')
                ? Colors.green
                : Colors.red;

    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      elevation: 2,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // Top Row: Recipient & Status
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Icon(isVendor ? Icons.store : Icons.person, size: 16, color: Colors.grey[700]),
                          const SizedBox(width: 4),
                          Expanded(
                            child: Text(
                              name,
                              style: const TextStyle(fontWeight: FontWeight.bold, fontSize: 15),
                              overflow: TextOverflow.ellipsis,
                            ),
                          ),
                        ],
                      ),
                      const SizedBox(height: 2),
                      Text(
                        '$trackingId • ID: ${id.length > 8 ? id.substring(0, 8) : id}',
                        style: TextStyle(fontSize: 11, color: Colors.grey[600]),
                      ),
                    ],
                  ),
                ),
                Container(
                  padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                  decoration: BoxDecoration(
                    color: statusColor.withValues(alpha: 0.12),
                    borderRadius: BorderRadius.circular(8),
                    border: Border.all(color: statusColor.withValues(alpha: 0.3)),
                  ),
                  child: Text(
                    status.toUpperCase(),
                    style: TextStyle(color: statusColor, fontSize: 11, fontWeight: FontWeight.bold),
                  ),
                ),
              ],
            ),
            const Divider(height: 20),

            // Bank & IBAN Box
            Container(
              padding: const EdgeInsets.all(10),
              decoration: BoxDecoration(
                color: Colors.grey.shade50,
                borderRadius: BorderRadius.circular(8),
                border: Border.all(color: Colors.grey.shade300),
              ),
              child: Row(
                children: [
                  const Icon(Icons.account_balance, size: 20, color: Colors.blueGrey),
                  const SizedBox(width: 8),
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(bankName, style: const TextStyle(fontWeight: FontWeight.w600, fontSize: 12)),
                        Text(
                          iban.isNotEmpty ? iban : 'No IBAN provided',
                          style: TextStyle(
                            fontSize: 12,
                            fontFamily: 'monospace',
                            color: iban.isNotEmpty ? Colors.black87 : Colors.red,
                          ),
                        ),
                      ],
                    ),
                  ),
                  if (iban.isNotEmpty)
                    IconButton(
                      icon: const Icon(Icons.copy, size: 16, color: Colors.grey),
                      tooltip: 'Copy IBAN',
                      onPressed: () {
                        Clipboard.setData(ClipboardData(text: iban));
                        ScaffoldMessenger.of(context).showSnackBar(
                          const SnackBar(content: Text('IBAN copied to clipboard'), duration: Duration(seconds: 1)),
                        );
                      },
                    ),
                ],
              ),
            ),
            const SizedBox(height: 12),

            // Amount Breakdown Row
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('Requested', style: TextStyle(fontSize: 11, color: Colors.grey[600])),
                    Text(
                      'PKR ${amount.toStringAsFixed(0)}',
                      style: const TextStyle(fontSize: 14, fontWeight: FontWeight.bold),
                    ),
                  ],
                ),
                Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('Fee', style: TextStyle(fontSize: 11, color: Colors.grey[600])),
                    Text(
                      'PKR ${feeRupees.toStringAsFixed(0)}',
                      style: TextStyle(fontSize: 14, color: Colors.grey[800]),
                    ),
                  ],
                ),
                Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('Net Disburse', style: TextStyle(fontSize: 11, color: Colors.grey[600])),
                    Text(
                      'PKR ${netRupees.toStringAsFixed(0)}',
                      style: const TextStyle(fontSize: 15, fontWeight: FontWeight.bold, color: Colors.green),
                    ),
                  ],
                ),
                Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text('Gateway', style: TextStyle(fontSize: 11, color: Colors.grey[600])),
                    Text(
                      gateway.toUpperCase(),
                      style: const TextStyle(fontSize: 12, fontWeight: FontWeight.bold, color: Colors.deepPurple),
                    ),
                  ],
                ),
              ],
            ),

            if (trace.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text('Safepay Trace: $trace', style: const TextStyle(fontSize: 11, color: Colors.teal, fontFamily: 'monospace')),
            ],
            if (failureReason.isNotEmpty) ...[
              const SizedBox(height: 8),
              Text('Failure: $failureReason', style: const TextStyle(fontSize: 11, color: Colors.red)),
            ],

            const SizedBox(height: 10),
            Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                Text(
                  'Created: ${createdAt.contains("T") ? createdAt.split("T").first : createdAt}',
                  style: TextStyle(fontSize: 11, color: Colors.grey[500]),
                ),
              ],
            ),

            // Action Buttons
            if (status == 'pending' || status == 'scheduled' || status == 'approved') ...[
              const SizedBox(height: 12),
              Row(
                children: [
                  // Disburse via Safepay Button
                  Expanded(
                    flex: 3,
                    child: ElevatedButton.icon(
                      onPressed: () => _disburseSafepay(id, isVendor),
                      icon: const Icon(Icons.bolt, size: 16, color: Colors.white),
                      label: const Text('Pay via Safepay', style: TextStyle(fontSize: 12, color: Colors.white)),
                      style: ElevatedButton.styleFrom(backgroundColor: Colors.deepPurple),
                    ),
                  ),
                  const SizedBox(width: 6),
                  if (status != 'approved') ...[
                    Expanded(
                      flex: 2,
                      child: OutlinedButton.icon(
                        onPressed: () => _approvePayout(id, isVendor),
                        icon: const Icon(Icons.check, size: 14),
                        label: const Text('Approve', style: TextStyle(fontSize: 12)),
                        style: OutlinedButton.styleFrom(foregroundColor: Colors.blue),
                      ),
                    ),
                    const SizedBox(width: 6),
                  ],
                  Expanded(
                    flex: 2,
                    child: OutlinedButton.icon(
                      onPressed: () => _rejectPayout(id, isVendor),
                      icon: const Icon(Icons.close, size: 14),
                      label: const Text('Reject', style: TextStyle(fontSize: 12)),
                      style: OutlinedButton.styleFrom(foregroundColor: Colors.red),
                    ),
                  ),
                ],
              ),
            ],
          ],
        ),
      ),
    );
  }
}
