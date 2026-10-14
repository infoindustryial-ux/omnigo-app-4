import 'package:flutter/material.dart';
import 'package:payfast_flutter_sdk/payfast_flutter_sdk.dart';

void main() {
  runApp(const PayfastExampleApp());
}

class PayfastExampleApp extends StatelessWidget {
  const PayfastExampleApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Payfast SDK Example',
      theme: ThemeData(
        colorSchemeSeed: const Color(0xFF0B4F9E),
        useMaterial3: true,
      ),
      home: const CheckoutScreen(),
    );
  }
}

class CheckoutScreen extends StatefulWidget {
  const CheckoutScreen({super.key});

  @override
  State<CheckoutScreen> createState() => _CheckoutScreenState();
}

class _CheckoutScreenState extends State<CheckoutScreen> {
  final _formKey = GlobalKey<FormState>();

  // Replace these with the credentials issued by Payfast. In a real app they
  // belong in secure configuration, never in source control.
  final _merchantId = TextEditingController();
  final _securedKey = TextEditingController();

  final _merchantName = TextEditingController(text: 'My Store');
  final _amount = TextEditingController(text: '1500.00');
  final _description = TextEditingController(text: 'Order payment');
  final _customerEmail = TextEditingController(text: 'customer@example.com');
  final _customerMobile = TextEditingController(text: '03001234567');
  final _successUrl =
      TextEditingController(text: 'https://merchant.example/success');
  final _failureUrl =
      TextEditingController(text: 'https://merchant.example/failure');
  final _checkoutUrl =
      TextEditingController(text: 'https://merchant.example/checkout');

  bool _isUAT = true;
  bool _isBusy = false;
  String? _outcome;
  bool _outcomeIsError = false;

  @override
  void dispose() {
    for (final controller in [
      _merchantId,
      _securedKey,
      _merchantName,
      _amount,
      _description,
      _customerEmail,
      _customerMobile,
      _successUrl,
      _failureUrl,
      _checkoutUrl,
    ]) {
      controller.dispose();
    }
    super.dispose();
  }

  String _newBasketId() =>
      'BASKET-${DateTime.now().millisecondsSinceEpoch}';

  void _report(String message, {bool isError = false}) {
    if (!mounted) return;
    setState(() {
      _outcome = message;
      _outcomeIsError = isError;
    });
  }

  Future<void> _pay() async {
    if (!_formKey.currentState!.validate()) return;

    setState(() {
      _isBusy = true;
      _outcome = null;
    });

    try {
      final service = RedirectionService.newBuilder()
          .setMerchantId(_merchantId.text.trim())
          .setSecuredKey(_securedKey.text.trim())
          .setBasketID(_newBasketId())
          // setTxnAmount keeps decimals; setAmount rounds to whole units.
          .setTxnAmount(_amount.text.trim())
          .setDescription(_description.text.trim())
          .setCustomerEmail(_customerEmail.text.trim())
          .setCustomerMobile(_customerMobile.text.trim())
          .setMerchantName(_merchantName.text.trim())
          .setSuccessURL(_successUrl.text.trim())
          .setFailureURL(_failureUrl.text.trim())
          .setCheckoutURL(_checkoutUrl.text.trim())
          .setIsUAT(_isUAT)
          .setSuccessListener((url) => _report('Payment successful.'))
          .setFailureListener(
            (url) => _report('Payment failed.', isError: true),
          )
          .setTokenErrorListener(
            (error) => _report('Token error: $error', isError: true),
          )
          .build();

      // Fetches the access token, then pushes the payment WebView route.
      await service.fetchTokenAndOpenWebView(context);
    } on ArgumentError catch (e) {
      _report('Invalid input: ${e.message}', isError: true);
    } catch (e) {
      _report('Unexpected error: $e', isError: true);
    } finally {
      if (mounted) setState(() => _isBusy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Payfast SDK Example')),
      body: Form(
        key: _formKey,
        child: ListView(
          padding: const EdgeInsets.all(16),
          children: [
            const _SectionTitle('Merchant credentials'),
            _field(_merchantId, 'Merchant ID'),
            _field(_securedKey, 'Secured key', obscure: true),
            _field(_merchantName, 'Merchant name'),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              title: const Text('UAT / sandbox'),
              subtitle: Text(
                _isUAT
                    ? 'ipguat.apps.net.pk — pair with UAT credentials'
                    : 'ipg1.apps.net.pk — pair with production credentials',
              ),
              value: _isUAT,
              onChanged: _isBusy
                  ? null
                  : (value) => setState(() => _isUAT = value),
            ),
            const _SectionTitle('Transaction'),
            _field(
              _amount,
              'Amount (PKR)',
              keyboardType: const TextInputType.numberWithOptions(
                decimal: true,
              ),
            ),
            _field(_description, 'Description'),
            const _SectionTitle('Customer'),
            _field(
              _customerEmail,
              'Email',
              keyboardType: TextInputType.emailAddress,
            ),
            _field(
              _customerMobile,
              'Mobile',
              keyboardType: TextInputType.phone,
            ),
            const _SectionTitle('Redirect URLs'),
            _field(_successUrl, 'Success URL'),
            _field(_failureUrl, 'Failure URL'),
            _field(_checkoutUrl, 'Checkout URL'),
            const SizedBox(height: 24),
            FilledButton(
              onPressed: _isBusy ? null : _pay,
              child: _isBusy
                  ? const SizedBox(
                      height: 20,
                      width: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Text('Pay now'),
            ),
            if (_outcome != null) ...[
              const SizedBox(height: 16),
              Card(
                color: _outcomeIsError
                    ? Theme.of(context).colorScheme.errorContainer
                    : Theme.of(context).colorScheme.secondaryContainer,
                child: Padding(
                  padding: const EdgeInsets.all(16),
                  child: Text(_outcome!),
                ),
              ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _field(
    TextEditingController controller,
    String label, {
    bool obscure = false,
    TextInputType? keyboardType,
  }) {
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: TextFormField(
        controller: controller,
        obscureText: obscure,
        keyboardType: keyboardType,
        enabled: !_isBusy,
        decoration: InputDecoration(
          labelText: label,
          border: const OutlineInputBorder(),
        ),
        validator: (value) =>
            (value == null || value.trim().isEmpty) ? '$label is required' : null,
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle(this.text);

  final String text;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(top: 8, bottom: 12),
      child: Text(text, style: Theme.of(context).textTheme.titleMedium),
    );
  }
}
